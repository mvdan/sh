// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package internal

import "slices"

// The helpers below manipulate indexed array variables held as a list of
// elements plus the index of each element when the array is sparse.
// A nil indexes slice means the array is dense: each element's index is its
// position in the list. See the expand.Variable.Indexes field.

// IndexedMax returns the maximum index of an indexed array,
// or -1 when the array is empty.
func IndexedMax(list []string, indexes []int) int {
	if len(indexes) > 0 {
		return indexes[len(indexes)-1]
	}
	return len(list) - 1
}

// SetIndexedElem returns list and indexes with the element at index k set,
// converting a dense array to a sparse one when setting index k would
// otherwise leave a hole. The index k must not be negative.
func SetIndexedElem(list []string, indexes []int, k int, val string) ([]string, []int) {
	if indexes == nil {
		if k < len(list) {
			list[k] = val
			return list, nil
		}
		if k == len(list) {
			return append(list, val), nil
		}
		indexes = make([]int, len(list), len(list)+1)
		for i := range indexes {
			indexes[i] = i
		}
	}
	pos, ok := slices.BinarySearch(indexes, k)
	if ok {
		list[pos] = val
		return list, indexes
	}
	list = slices.Insert(list, pos, val)
	indexes = slices.Insert(indexes, pos, k)
	return list, CanonicalIndexes(indexes)
}

// DeleteIndexedElem is like [SetIndexedElem], but unsetting the element at
// index k, which may leave a hole.
func DeleteIndexedElem(list []string, indexes []int, k int) ([]string, []int) {
	if indexes == nil {
		if k < 0 || k >= len(list) {
			return list, nil
		}
		if k == len(list)-1 {
			return list[:k], nil
		}
		indexes = make([]int, len(list))
		for i := range indexes {
			indexes[i] = i
		}
	}
	pos, ok := slices.BinarySearch(indexes, k)
	if !ok {
		return list, indexes
	}
	list = slices.Delete(list, pos, pos+1)
	indexes = slices.Delete(indexes, pos, pos+1)
	return list, CanonicalIndexes(indexes)
}

// CanonicalIndexes returns nil when indexes is simply 0, 1, 2, and so on,
// as a dense array should leave the indexes nil.
func CanonicalIndexes(indexes []int) []int {
	for i, k := range indexes {
		if k != i {
			return indexes
		}
	}
	return nil
}
