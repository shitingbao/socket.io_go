package redis

import (
	"log"
	"testing"
)

func testLog() {
}

func TestMaxHeap(t *testing.T) {
	list := []timePart{
		{Timeout: 9, Fc: testLog},
		{Timeout: 3, Fc: testLog},
		{Timeout: 7, Fc: testLog},
		{Timeout: 6, Fc: testLog},
		{Timeout: 5, Fc: testLog},
		{Timeout: 1, Fc: testLog},
		{Timeout: 10, Fc: testLog},
		{Timeout: 2, Fc: testLog},
	}
	h, err := NewMaxHeap(list)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("first:", h.List())
	log.Println(h.GetValue())
	log.Println("h:", h.List())
	h.putValue(timePart{Timeout: 8, Fc: testLog})
	h.putValue(timePart{Timeout: 14, Fc: testLog})
	log.Println(h.GetValue())
	log.Println("h:", h.List())
}

func TestMinHeap(t *testing.T) {
	list := []timePart{
		{Timeout: 9, Fc: testLog},
		{Timeout: 3, Fc: testLog},
		{Timeout: 7, Fc: testLog},
		{Timeout: 6, Fc: testLog},
		{Timeout: 5, Fc: testLog},
		{Timeout: 1, Fc: testLog},
		{Timeout: 10, Fc: testLog},
		{Timeout: 2, Fc: testLog},
	}
	h, err := NewMinHeap(list)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("first:", h.List())
	log.Println(h.GetValue())
	log.Println("h:", h.List())
	h.putValue(timePart{Timeout: 8, Fc: testLog})
	h.putValue(timePart{Timeout: 14, Fc: testLog})
	log.Println(h.GetValue())
	log.Println("h:", h.List())
}
