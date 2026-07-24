package main

import (
	"bytes"
	"encoding/binary"
	"iter"
	"math/bits"
)

type Stats struct {
	Count    int64
	Min, Max int32
	Sum      int64
}

type entry struct {
	key   []byte
	stats Stats
}

type HashTable struct {
	size  int
	table []entry
}

func NewHashTable(size int) *HashTable {
	if size <= 0 || size&(size-1) != 0 {
		panic("hash table size must be a positive power of two")
	}
	return &HashTable{table: make([]entry, size)}
}

func (ht *HashTable) Insert(key []byte, temp int32, hash uint64) {
	ht.insertStats(key, Stats{Count: 1, Min: temp, Max: temp, Sum: int64(temp)}, hash)
}

func (ht *HashTable) insertStats(key []byte, stats Stats, hash uint64) {
	mask := uint64(len(ht.table) - 1)
	index := hash & mask
	for {
		if ht.table[index].key == nil {
			copiedKey := make([]byte, len(key))
			copy(copiedKey, key)
			ht.table[index] = entry{key: copiedKey, stats: stats}
			ht.size++
			return
		}
		if bytes.Equal(ht.table[index].key, key) {
			current := &ht.table[index].stats
			current.Count += stats.Count
			current.Sum += stats.Sum
			if stats.Min < current.Min {
				current.Min = stats.Min
			}
			if stats.Max > current.Max {
				current.Max = stats.Max
			}
			return
		}
		index = (index + 1) & mask
	}
}

func (ht *HashTable) Merge(other *HashTable) {
	for i := range other.table {
		entry := &other.table[i]
		if entry.key != nil {
			ht.insertStats(entry.key, entry.stats, hash(entry.key))
		}
	}
}

const stationHashSeed = 0x9e3779b185ebca87

func mixStationHash(hash, word uint64) uint64 {
	hash ^= word * 0xc2b2ae3d27d4eb4f
	hash = bits.RotateLeft64(hash, 31)
	return hash*0x9e3779b185ebca87 + 0x165667b19e3779f9
}

func finishStationHash(hash uint64, length int) uint64 {
	hash ^= uint64(length) * 0x27d4eb2f165667c5
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33
	return hash
}

func hash(key []byte) uint64 {
	hash := uint64(stationHashSeed)
	length := len(key)
	for len(key) >= 8 {
		hash = mixStationHash(hash, binary.LittleEndian.Uint64(key))
		key = key[8:]
	}
	if len(key) > 0 {
		var tail uint64
		for i, char := range key {
			tail |= uint64(char) << (8 * i)
		}
		hash = mixStationHash(hash, tail)
	}
	return finishStationHash(hash, length)
}

func (ht *HashTable) Get(key []byte) Stats {
	mask := uint64(len(ht.table) - 1)
	index := hash(key) & mask
	for {
		if ht.table[index].key == nil {
			return Stats{}
		}
		if bytes.Equal(ht.table[index].key, key) {
			return ht.table[index].stats
		}
		index = (index + 1) & mask
	}
}

func (ht *HashTable) Keys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, entry := range ht.table {
			if entry.key != nil && !yield(string(entry.key)) {
				return
			}
		}
	}
}

func (ht *HashTable) All() iter.Seq2[string, Stats] {
	return func(yield func(string, Stats) bool) {
		for _, entry := range ht.table {
			if entry.key != nil && !yield(string(entry.key), entry.stats) {
				return
			}
		}
	}
}

func (ht *HashTable) Len() int {
	return ht.size
}
