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
	qs := buildQueues()
	trans := createTransfer("transfer", 0, nil, 0, nil)
	qs.add("one", trans)
	qs.add("one", nil) // shouldn't be added
	qs.add("one", trans)
	qs.add("two", trans)
	qs.add("four", trans)
	// now try to retreive them
	if nil == qs.get("one") {
		t.Error("Expected transfer, got nil")
	}
	if nil == qs.get("two") {
		t.Error("Expected transfer, got nil")
	}
	if nil == qs.get("four") {
		t.Error("Expected transfer, got nil")
	}
	if nil != qs.get("two") {
		t.Error("Expected nil, got transfer")
	}
	if nil != qs.get("four") {
		t.Error("Expected nil, got transfer")
	}
	if nil != qs.get("three") {
		t.Error("Expected nil, got transfer")
	}
	if nil == qs.get("one") {
		t.Error("Expected transfer, got nil")
	}
	if nil != qs.get("one") {
		t.Error("Expected nil, got transfer")
	}
}
