package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
)

const tickerDuration = 30

type entry struct {
	done chan struct{}
	err  error
}

type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func NewRegistry() *Registry {
	return &Registry{
		entries: map[string]*entry{},
	}
}

func (r *Registry) Start(id string) (*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := r.entries[id]; ok {
		return v, ok
	}
	//start if it doens't
	e := &entry{done: make(chan struct{})}
	r.entries[id] = e
	return e, false
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

func (r *Registry) Get(id string) (*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	return e, ok
}

// Finish saves the result, awakes who is listening and remove the entry
func (r *Registry) Finish(id string, err error) {
	r.mu.Lock()
	e, ok := r.entries[id]
	delete(r.entries, id)
	r.mu.Unlock()
	// it didn't exist
	if !ok {
		return
	}
	//if the key existed.
	e.err = err
	close(e.done)
}

type job struct {
	ID     string
	URL    string
	Width  uint
	Height uint
	Ctx context.Context
}

type WorkerPool struct {
	jobs  chan job
	cache *lru.Cache
	regis *Registry
}

func (w *WorkerPool) Submit(j job) bool {
	select {
	case w.jobs <- j:
		return true
	default:
		return false //if buffer is full
	}
}

func NewWorkerPool(nWorkers int, cache *lru.Cache, regis *Registry) *WorkerPool {
	ch := make(chan job, 50)
	w := WorkerPool{ch, cache, regis}
	log.Printf("launching %d workers ", nWorkers)
	for i := 0; i < nWorkers; i++ {
		go w.worker()
	}
	return &w
}

func (w *WorkerPool) worker() {
	for job := range w.jobs {
		w.run(job)
	}
}

func (w *WorkerPool) run(j job){
	var data []byte
	var err error

	defer func(){
		if r:= recover(); r!=nil{
			data, err = nil, fmt.Errorf("panic: %v", r)
		}
		if err ==nil{
			w.cache.Add(j.ID, ImageInfo{data, time.Now()})
		}else{
			log.Printf("failed to resize %s - %s: %v", j.URL, j.ID, err )
		}
		w.regis.Finish(j.ID, err)
	}()

	data, err = fetchAndResize(context.WithoutCancel(j.Ctx), j.URL, j.Width, j.Height)
}

type ImageInfo struct{
	Img []byte
	LastUsed time.Time
}
//pooling each 1 minutes
func cacheCleanUp(cache *lru.Cache){
	//iterate over the cache
	ticker := time.NewTicker(tickerDuration * time.Second)

	go func(){
		for t:= range ticker.C{
			fmt.Print("tick at", t)
			cacheIterator(cache)
		}
	}()
}


func cacheIterator(cache *lru.Cache){
	for _,id := range cache.Keys(){
		value, ok := cache.Get(id)
		if !ok{
			continue
		}
		imgData, ok:= value.(ImageInfo)
		if !ok{
			continue
		}
		if time.Since(imgData.LastUsed)>60*time.Second{
			cache.Remove(id)
		}
	}
}