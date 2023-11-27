package redis

import (
	"log"
	"testing"
)

func testLog() {
}

func TestMaxHeap(t *testing.T) {
	list := []timePart{
		{Key: "9", Timeout: 9, Fc: testLog},
		{Key: "3", Timeout: 3, Fc: testLog},
		{Key: "7", Timeout: 7, Fc: testLog},
		{Key: "6", Timeout: 6, Fc: testLog},
		{Key: "5", Timeout: 5, Fc: testLog},
		{Key: "1", Timeout: 1, Fc: testLog},
		{Key: "10", Timeout: 10, Fc: testLog},
		{Key: "2", Timeout: 2, Fc: testLog},
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
		{Key: "9", Timeout: 9, Fc: testLog},
		{Key: "3", Timeout: 3, Fc: testLog},
		{Key: "7", Timeout: 7, Fc: testLog},
		{Key: "6", Timeout: 6, Fc: testLog},
		{Key: "5", Timeout: 5, Fc: testLog},
		{Key: "1", Timeout: 1, Fc: testLog},
		{Key: "10", Timeout: 10, Fc: testLog},
		{Key: "2", Timeout: 2, Fc: testLog},
	}
	h, err := NewMinHeap(list...)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("first:", h.List())
	log.Println(h.GetValue())
	log.Println("h:", h.List())
	h.Delete("5")
	h.Delete("10")
	h.putValue(timePart{Timeout: 8, Fc: testLog})
	h.putValue(timePart{Timeout: 14, Fc: testLog})
	log.Println(h.GetValue())
	log.Println("h:", h.List())
}
