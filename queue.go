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
	// don't add nil values
	if t == nil {
		return
	}
	_, exists := qs.heap[address]
	// if queue for address doesn't exist yet, build it
	if !exists {
		qs.heap[address] = buildQueue()
	}
	// add to queue
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

const capacity = 4

func buildQueue() *queue {
	return &queue{entries: make([]*transfer, 0, capacity)}
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
		q.entries = make([]*transfer, 0, capacity)
	} else {
		// length > 1, so rewrite slice
		q.entries = q.entries[1:]
	}
	return t
}

func (q *queue) length() int {
	return len(q.entries)
}
