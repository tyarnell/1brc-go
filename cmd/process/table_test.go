package main

import (
	"maps"
	"testing"
)

func TestHashTable(t *testing.T) {
	ht := NewHashTable(1024)

	t.Run("Insert and Retrieve", func(t *testing.T) {
		key := []byte("StationA")
		hash := hash(key)
		temp := int32(250)

		ht.Insert(key, temp, hash)

		entries := maps.Collect(ht.All())
		found := false
		for station, stats := range entries {
			if station == "StationA" {
				found = true
				if stats.Count != 1 || stats.Min != temp || stats.Max != temp || stats.Sum != int64(temp) {
					t.Errorf("Stats mismatch: got %+v", stats)
				}
				break
			}
		}
		if !found {
			t.Errorf("Entry for StationA not found")
		}
	})

	t.Run("Update Existing Entry", func(t *testing.T) {
		key := []byte("StationA")
		hash := hash(key)
		temp := int32(300)

		ht.Insert(key, temp, hash)

		entries := maps.Collect(ht.All())
		for station, stats := range entries {
			if station == "StationA" {
				if stats.Count != 2 || stats.Min != 250 || stats.Max != temp || stats.Sum != 550 {
					t.Errorf("Stats mismatch after update: got %+v", stats)
				}
				break
			}
		}
		if ht.Len() != 1 {
			t.Errorf("Len() = %d, want 1 station", ht.Len())
		}
	})

	t.Run("Merge Aggregates Statistics", func(t *testing.T) {
		other := NewHashTable(1024)
		other.Insert([]byte("StationA"), 100, hash([]byte("StationA")))
		other.Insert([]byte("StationB"), -50, hash([]byte("StationB")))

		ht.Merge(other)

		if got := ht.Get([]byte("StationA")); got != (Stats{Count: 3, Min: 100, Max: 300, Sum: 650}) {
			t.Errorf("merged StationA stats = %+v", got)
		}
		if got := ht.Get([]byte("StationB")); got != (Stats{Count: 1, Min: -50, Max: -50, Sum: -50}) {
			t.Errorf("merged StationB stats = %+v", got)
		}
		if ht.Len() != 2 {
			t.Errorf("Len() = %d, want 2 stations", ht.Len())
		}
	})

	t.Run("Merge Billion Row Statistics", func(t *testing.T) {
		key := []byte("StationC")
		left := NewHashTable(1024)
		left.insertStats(key, Stats{Count: 500_000_000, Min: -999, Max: 998, Sum: 499_000_000_000}, hash(key))
		right := NewHashTable(1024)
		right.insertStats(key, Stats{Count: 500_000_000, Min: -998, Max: 999, Sum: 500_000_000_000}, hash(key))

		left.Merge(right)

		want := Stats{Count: 1_000_000_000, Min: -999, Max: 999, Sum: 999_000_000_000}
		if got := left.Get(key); got != want {
			t.Errorf("merged billion-row stats = %+v, want %+v", got, want)
		}
	})
}
