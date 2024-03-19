package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"sync"
)

type nodeType int

// oh go, where art thy sum types? thine enums forsake me :(
const (
	ROOT_NODE nodeType = iota
	INTERNAL_NODE
	LEAF_NODE
)

// invariant three: relationship between keys and child pointers
// every node (ie leaf or internal) except the root must have:
// at least MIN_DEGREE children
// todo: Assert this in split
var MIN_DEGREE_NODE int

type BPlusTree struct {
	root      *node
	maxDegree int

	// TODO: idk this use of indirect cyclical pointers feels bad
	db *DB
	sync.RWMutex
}

type node struct {
	kind     nodeType
	pageId   int64
	keys     []int
	next     *node
	previous *node
	parent   *node
	children []*node
}

// NB: terminology
// Inconsistencies, book pg. 78 "B-Tree Hierarchy"
// B-Trees are characterized by their fanout: the number of keys stored in each node
// else where:
// B+ TREE: FANOUT. Fanout: the number of pointers to child nodes coming out of a node.
// see: https://pages.cs.wisc.edu/~paris/cs564-s18/lectures/lecture-14.pdf

// degree relates to number of children = maxKeys + 1
// which relates to the branching factor (bound on children)
// branching factor can be expressed as maxDegree, and is the inequality
// b - 1 <= num keys < (2 * b) - 1

func NewBPlusTree(maxDegree int) *BPlusTree {
	// invariant one
	Assert(maxDegree >= 2, "the minimum maxDegree of a B+ tree must be greater than 2")

	// invariant two: relationship between keys and child pointers
	// Each node holds up to N keys and N + 1 pointers to the child nodes
	var MAX_KEY = maxDegree - 1

	// root node is initially empty and triggers no page allocation.
	return &BPlusTree{
		root: &node{
			kind:     ROOT_NODE,
			keys:     make([]int, MAX_KEY),
			children: make([]*node, maxDegree),
			next:     nil,
			previous: nil,
			parent:   nil,
			pageId:   0,
		},
		maxDegree: maxDegree,
	}
}

func (t *BPlusTree) Insert(key int, value []byte) error {
	t.Lock()
	defer t.Unlock()

	return t.root.insert(t, t.db.datafile, key, value, t.maxDegree)
}

func (t *BPlusTree) Search(key int) ([]byte, error) {
	t.RLock()
	defer t.RUnlock()

	return t.root.search(t, t.db.datafile, key)
}

func (t *BPlusTree) Delete(key int) error {
	t.Lock()
	defer t.Unlock()

	return t.root.delete(t, t.db.datafile, key, t.maxDegree)
}

func (n *node) insert(t *BPlusTree, file *os.File, key int, value []byte, maxDegree int) error {
	switch n.kind {
	case ROOT_NODE:
		if len(n.keys) > maxDegree {
			n.splitChild(t, len(n.keys)/2, t.maxDegree)
		} else {
			offset, err := syncToOffset(file, value)
			if err != nil {
				return err
			}
			n.pageId = offset

			// TODO: this key thing
			// do you map offsets to the id directly?
			n.keys = append(n.keys, int(offset))
		}
	case LEAF_NODE:
		i := 0
		for i < len(n.keys) && key > n.keys[i] {
			i++
		}

		offset, err := syncToOffset(file, value)

		if err != nil {
			return err
		}

		n.keys = append(n.keys, int(offset))
		slices.Sort(n.keys)
		copy(n.keys[i+1:], n.keys[i:])
		n.keys[i] = key
	case INTERNAL_NODE:
		i := 0
		for i < len(n.keys) && key > n.keys[i] {
			i++
		}
		n.children[i].insert(t, file, key, value, maxDegree)
	}

	if len(n.keys) > maxDegree {
		// wtf
		n.splitChild(t, 0, t.maxDegree)
	}

	return nil
}

func (node *node) search(t *BPlusTree, file *os.File, key int) ([]byte, error) {
	fmt.Println(node.kind)
	fmt.Println(node.keys)
	reader := bufio.NewReader(file)

	if node.kind == ROOT_NODE {
		for _, k := range node.keys {
			if k == key {
				fmt.Println("Iam not crazy")
				offset := int64(binary.BigEndian.Uint64([]byte(strconv.Itoa(k))))
				if _, err := file.Seek(offset, io.SeekStart); err != nil {
					return nil, err
				}

				value, err := reader.ReadBytes('\n')

				if err != nil {
					return value, nil
				}
			}
		}
	}

	// Binary search for the key
	if node.kind == LEAF_NODE {
		low, high := 0, len(node.keys)-1

		for low <= high {
			mid := low + (high-low)/2
			if node.keys[mid] == key {
				// Calculate the offset and seek to it
				offset := int64(binary.BigEndian.Uint64([]byte(strconv.Itoa(node.keys[mid]))))
				if _, err := file.Seek(offset, io.SeekStart); err != nil {
					return nil, err
				}

				// TODO: factor out when using binary format
				// read to delimiter
				value, err := reader.ReadBytes('\n')
				if err != nil {
					return nil, err
				}

				return value, nil
			} else if node.keys[mid] < key {
				low = mid + 1
			} else {
				high = mid - 1
			}
		}
		return nil, errors.New("key not found")
	}

	/*
		// If the node is not a leaf node, recursively search in the appropriate child
		i := 0
		for i < len(node.keys) && key >= node.keys[i] {
			i++
		}

		return node.children[i].search(t, key) // Recursively search in child node
	*/

	return nil, nil
}

func (node *node) delete(t *BPlusTree, file *os.File, key int, maxDegree int) error {
	// find node using key
	// pass in node, to node stealSibling
	// assume node is root at first
	// this an optimisation maybe do later
	ok, err := node.stealSibling(t, maxDegree)

	if err != nil {
		return err
	}

	if !ok {
		err := node.mergeChildren(t, maxDegree)

		if err != nil {
			return err
		}
	}

	return nil
}

func (node *node) stealSibling(t *BPlusTree, maxDegree int) (bool, error) {
	return false, nil
}

func (node *node) mergeChildren(t *BPlusTree, maxDegree int) error {
	return nil
}

// splitChild splits the child n of the current n at the specified index.
func (n *node) splitChild(t *BPlusTree, index, maxDegree int) {
	// Create a new n to hold the keys and children that will be moved
	newNode := &node{
		keys:     make([]int, t.maxDegree-1),
		children: make([]*node, t.maxDegree),
		kind:     LEAF_NODE,
		next:     n.next,
	}

	// Move keys and children to the new n
	copy(newNode.keys, n.keys[index:])
	copy(newNode.children, n.children[index:])
	n.keys = n.keys[:index]
	n.children = n.children[:index]

	// TODO(FIXME): next is not set correctly
	if n.kind == LEAF_NODE {
		n.next = newNode
	}

	// If the current n is the root, create a new root and add the median key
	if n.kind == ROOT_NODE {
		newRoot := &node{
			keys:     []int{newNode.keys[0]},
			children: []*node{n, newNode},
			kind:     ROOT_NODE,
		}
		t.root = newRoot
		return
	}

	// Otherwise, insert the median key into the parent n
	parent := n.parent
	parent.keys = append(parent.keys, 0)
	parent.children = append(parent.children, nil)
	copy(parent.keys[index+1:], parent.keys[index:])
	copy(parent.children[index+1:], parent.children[index:])
	parent.keys[index] = newNode.keys[0]
	parent.children[index] = newNode

	// Check if the parent n needs splitting
	if len(parent.keys) > maxDegree {
		parent.splitChild(t, len(parent.keys)/2, maxDegree)
	}
}

func syncToOffset(file *os.File, value []byte) (int64, error) {
	writer := bufio.NewWriter(file)
	// seek to correct block position using the pageID
	// make sure we get to disk
	_, err := writer.Write(value)
	fErr := writer.Flush()

	if err != nil {
		return 0, err
	}

	if fErr != nil {
		return 0, fErr
	}

	offset, err := file.Seek(0, io.SeekCurrent)

	if err != nil {
		return 0, err
	}

	return offset, nil
}
