package channel

import (
	"fmt"
	"testing"
)

func TestQueue(t *testing.T) {
	q := buildQueue()
	// write is how many transfer we want to write
	write := 11
	for index := 0; index < write; index++ {
		trans := createTransfer(fmt.Sprintf("test%d", index), uint32(index), nil, 0, nil)
		q.add(trans)
	}
	// check length
	if q.length() != write {
		t.Error("Expected length of", write, "got", q.length())
	}
	// check get until empty and beyond
	for index := 0; index < 14; index++ {
		trans := q.get()
		if index < 11 {
			if trans == nil {
				t.Error("Expected transfer, got", trans)
			}
			continue
		}
		if trans != nil {
			t.Error("Expected nil, got", trans)
		}
	}
}

func TestQueues(t *testing.T) {
	t.Fatal("Unimplemented")
}
