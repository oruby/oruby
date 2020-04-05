package thread

import (
	"container/list"
	"github.com/oruby/oruby"
	"runtime"
	"sync"
)

type queue struct {
	sync.Mutex
	mrb *oruby.MrbState
	ch chan interface{}
	items *list.List
	closed bool
}

type sizedQueue struct {
	queue
}

func newQueue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	q := &queue{
		sync.Mutex{},
		mrb,
		make(chan interface{}),
		list.New(),
		false,
	}

	go q.worker()

	mrb.DataSetInterface(self, q)
	return self
}

func newSizedQueue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	size := mrb.GetArgsFirst().Int()
	q := &sizedQueue{
		queue{
			sync.Mutex{},
			mrb,
			make(chan interface{}, size),
			list.New(),
			false,
		},
	}

	go q.worker()

	mrb.DataSetInterface(self, q)
	return self
}

func (q *queue) worker() {
	for {
		el := q.fetchFirst()
		if el == nil {
			runtime.Gosched()
			continue
		}

		select {
		case q.ch <- el.Value:
			runtime.Gosched()
		case <-q.mrb.ExitChan():
			return
		}
	}
}

func (q *queue) fetchFirst() *list.Element {
	q.Lock()
	defer q.Unlock()

	el := q.items.Front()
	if el != nil {
		q.items.Remove(el)
	}

	return el
}

// Clear queue
func (q *queue) Clear() {
	q.Lock()
	q.items.Init()
	for len(q.ch) > 0 {
		<-q.ch
	}
	q.Unlock()
}

// IsCLosed returns true if queue is closed
func (q *queue) IsClosed() bool  {
	if q.closed {
		return true
	}

	select {
	case <-q.ch:
		q.closed = true
	default:
		q.closed = q.Size() > 0
	}
	return q.closed
}

// Close queue
func (q *queue) Close() {
	q.closed = true
}

// IsEmpty returns true if queue is empty
func (q *queue) IsEmpty() bool {
	return q.Size() == 0
}

// Size returns number of items waiting
func (q *queue) Size() int {
	q.Lock()
	defer q.Unlock()

	return q.items.Len()
}

// Push interface to queue.
func (q *queue) Push(obj interface{}) error {
	if q.closed {
		return oruby.EError("ClosedQueueError", "Queue is closed")
	}

	q.Lock()
	q.items.PushBack(obj)
	q.Unlock()

	return nil
}

// Pop interface from queue. Blocks and waits if queue is empty
func (q *queue) Pop(nonBlock *bool) (interface{}, error) {
	if nonBlock != nil && *nonBlock {
		select {
		case msg := <-q.ch:
			return msg, nil
		default:
			return nil, oruby.EError("ThreadError", "Queue is blocked")
		}

	}
	return <-q.ch, nil
}
