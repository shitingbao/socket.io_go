// see from https://github.com/shitingbao/heap
package redis

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
)

var _ sync.Locker = (*spinLock)(nil)

type spinLock uint32

const maxBackoff = 16

func (sl *spinLock) Lock() {
	backoff := 1
	for !atomic.CompareAndSwapUint32((*uint32)(sl), 0, 1) {
		for i := 0; i < backoff; i++ {
			runtime.Gosched()
		}
		if backoff < maxBackoff {
			backoff <<= 1
		}
	}
}

func (sl *spinLock) Unlock() {
	atomic.StoreUint32((*uint32)(sl), 0)
}

func NewSpinLock() sync.Locker {
	return new(spinLock)
}

type MinHeap struct {
	*heap
}

type MaxHeap struct {
	*heap
}

type heap struct {
	list   []timePart
	symbol bool
	lock   sync.Locker
}

func NewMaxHeap(list []timePart) (*MaxHeap, error) {
	h := &heap{symbol: true, list: make([]timePart, len(list)), lock: NewSpinLock()}
	h.construct(list)
	return &MaxHeap{h}, nil
}

func NewMinHeap(l ...timePart) (*MinHeap, error) {
	h := &heap{symbol: false, list: make([]timePart, len(l)), lock: NewSpinLock()}
	h.construct(l)
	return &MinHeap{h}, nil
}

// timePartIds
func (h *heap) Delete(list ...string) {
	h.lock.Lock()
	defer h.lock.Unlock()
	m := make(map[string]bool)
	for _, k := range list {
		m[k] = true
	}
	for i, v := range h.List() {
		if m[v.Key] {
			h.list = append(h.list[0:i], h.list[i+1:]...)
		}
	}
	h.construct(h.list)
}

func (h *heap) construct(list []timePart) {
	for i, v := range list {
		h.list[i] = v
		h.upSort(i)
	}
}

func (h *heap) TryGetValue() (timePart, error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.tryGetValue()
}

func (h *heap) tryGetValue() (timePart, error) {
	if len(h.list) == 0 {
		return timePart{}, errors.New("heap is no val")
	}
	p := h.list[0]
	return p, nil
}

func (h *heap) GetValue() (timePart, error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.getValue()
}

func (h *heap) getValue() (timePart, error) {
	p, err := h.tryGetValue()
	if err != nil {
		return timePart{}, err
	}
	h.list[0], h.list[len(h.list)-1] = h.list[len(h.list)-1], h.list[0]
	h.list = h.list[:len(h.list)-1]
	h.lowSort(0)
	return p, nil
}

func (h *heap) PutValue(val timePart) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.putValue(val)
}

func (h *heap) putValue(val timePart) {
	h.list = append(h.list, val)
	h.upSort(len(h.list) - 1)
}

func (h *heap) List() []timePart {
	return h.list
}

func (h *heap) upSort(flag int) {
	for {
		if flag <= 0 {
			break
		}
		index := (flag - 1) / 2
		if (flag)%2 == 0 {
			index = (flag - 2) / 2
		}
		switch {
		case h.symbol:
			if (h.list)[index].Timeout < (h.list)[flag].Timeout {
				(h.list)[index], (h.list)[flag] = (h.list)[flag], (h.list)[index]
			}
		default:
			if (h.list)[index].Timeout > (h.list)[flag].Timeout {
				(h.list)[index], (h.list)[flag] = (h.list)[flag], (h.list)[index]
			}
		}
		flag = index
	}
}

func (h *heap) lowSort(flag int) {
	for {
		left := flag*2 + 1
		if left > len(h.list)-1 {
			break
		}
		right := flag*2 + 2
		index := left
		switch {
		case h.symbol:
			if right <= len(h.list)-1 && (h.list)[left].Timeout < (h.list)[right].Timeout {
				index = right
			}
			if (h.list)[index].Timeout > (h.list)[flag].Timeout {
				(h.list)[index], (h.list)[flag] = (h.list)[flag], (h.list)[index]
			}
		default:
			if right <= len(h.list)-1 && (h.list)[left].Timeout > (h.list)[right].Timeout {
				index = right
			}
			if (h.list)[index].Timeout < (h.list)[flag].Timeout {
				(h.list)[index], (h.list)[flag] = (h.list)[flag], (h.list)[index]
			}
		}
		flag = index
	}
}
