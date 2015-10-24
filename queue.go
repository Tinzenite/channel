package channel

/*
queues is a collection of queues.
*/
type queues struct {
	heap map[string]*queue
}

func buildQueues() *queues {
	return &queues{heap: make(map[string]*queue)}
}

func (qs *queues) add(address string, t *transfer) {
	_, exists := qs.heap[address]
	if !exists {
		qs.heap[address] = buildQueue()
	}
	qs.heap[address].add(t)
}

func (qs *queues) get(address string) *transfer {
	_, exists := qs.heap[address]
	if !exists {
		return nil
	}
	return qs.heap[address].get()
}

/*
queue is a single queue.
*/
type queue struct {
	entries []*transfer
}

// TODO check if len 1 is max cap... not good. Also set to good value.
func buildQueue() *queue {
	return &queue{entries: make([]*transfer, 1)}
}

func (q *queue) add(t *transfer) {
	q.entries = append(q.entries, t)
}

func (q *queue) get() *transfer {
	// if zero none to get
	if len(q.entries) == 0 {
		return nil
	}
	// otherwise one to get
	t := q.entries[0]
	// if length is one only one to return
	if len(q.entries) == 1 {
		// make entries empty
		q.entries = make([]*transfer, 1)
	} else {
		// length > 1, so rewrite slice
		q.entries = q.entries[1:]
	}
	return t
}
