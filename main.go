package main

import (
	"log"
	"net/http"
	"runtime"

	lru "github.com/hashicorp/golang-lru"
)

type service struct {
	cache *lru.Cache
	regis *Registry
	wp    *WorkerPool
}

type resizeRequest struct {
	URLs   []string `json:"urls"`
	Width  uint     `json:"width"`
	Height uint     `json:"height"`
}

type resizeResult struct {
	Result string `json:"result"`
	ID     string `json:"id"`
	URL    string `json:"url,omitempty"`
	Cached bool   `json:"cached"`
}

const (
	proto    = "http://"
	hostport = "localhost:8080"
	success  = "success"
	failure  = "failure"
)

func main() {

	svc := newService(runtime.NumCPU())
	mux := svc.routes()

	address := hostport

	log.Print("Listening on ", hostport)
	// When running on docker mac, can't listen only on localhost
	panic(http.ListenAndServe(address, mux))
}

func newService(workers int) *service {
		cache, err := lru.New(1024)
	if err != nil {
		log.Panicf("Faild to create cache: %v", err)
	}
	
	cacheCleanUp(cache)

	regis:= NewRegistry()
	return &service{
		cache: cache,
		regis: regis,
		wp:    NewWorkerPool(workers, cache, regis),
	}
}

func (s * service) routes() http.Handler{
	mux := http.NewServeMux()
	mux.Handle("/v1/resize", s.resizeHandler())
	mux.Handle("/v1/image/{id}/image.jpeg", s.getImageHandler())
	mux.Handle("/v1/images/{ids}/archive.zip", s.getImagesZipHandler())

	return mux
}