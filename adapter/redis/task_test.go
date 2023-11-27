package redis

import (
	"log"
	"testing"
	"time"
)

func TestTask(t *testing.T) {
	task := NewDefaultTask()
	defer task.Close()

	task.Set(time.Second, func() { log.Println("111") })
	task.Set(time.Second*2, func() { log.Println("222") })
	t3 := task.Set(time.Second*3, func() { log.Println("333") })
	t4 := task.Set(time.Second*4, func() { log.Println("444") })
	task.Set(time.Second*5, func() { log.Println("555") })
	task.Set(time.Second*6, func() { log.Println("666") })
	task.Set(time.Second*7, func() { log.Println("777") })

	task.Clear(t3)
	task.Clear(t4)
	time.Sleep(time.Second * 10)
}
