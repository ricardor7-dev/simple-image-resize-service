package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const timeOutDef = 30 * time.Second
const maxDim = 1000

func (s *service) resizeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeOutDef)
		defer cancel()

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Expecting POST request"))
			return
		}

		request := resizeRequest{}
		err := json.NewDecoder(io.LimitReader(r.Body, 8*1024)).Decode(&request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Failed to parse request"))
			return
		}

		//validate here
		if request.Height> maxDim || request.Width>maxDim{
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("dimensions above the max limit 1000"))
			return
		}

		var results []resizeResult

		rawAsync := r.URL.Query().Get("async")
		async, _ := strconv.ParseBool(rawAsync)
		log.Printf("resize request: %d urls, async=%v", len(request.URLs), async)

		status := http.StatusCreated
		if async {
			results, err = s.processResizesAsync(ctx, request)
			status = http.StatusAccepted
		} else {
			results, err = s.processResizes(ctx, request)
		}

		if errors.Is(err, context.DeadlineExceeded) {
			log.Print("timeout for the resize")
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to process request"))
			return
		}

		data, err := json.Marshal(results)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to marshal response"))
			return
		}

		w.Header().Add("content-type", "application/json")
		w.WriteHeader(status)
		w.Write(data)
	}
}

func (s *service) getImageHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeOutDef)
		defer cancel()

		id := r.PathValue("id")

		regist, ok := s.regis.Get(id)
		if ok {
			log.Print("waiting, processing image ", id)
			select {
			case <-regist.done:
				if regist.err != nil {
					log.Printf("image %s failed: %v", id, regist.err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					log.Print("timeout waiting for image ", id)
					w.WriteHeader(http.StatusGatewayTimeout)
					return
				}
				log.Print("client canceled request ", id)
				return
			}
		}

		v, ok := s.cache.Get(id)
		if !ok{
			w.WriteHeader(http.StatusNotFound)
			return
		}

		imgData := v.(ImageInfo)
		imgData.LastUsed = time.Now()
		s.cache.Add(id, imgData)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Add("content-type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imgData.Img)
	})
}

func (s *service) getImagesZipHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeOutDef)
		defer cancel()
		ids := parseIDs(r)

		log.Println("fetching ", ids, "zip")

		w.Header().Set("content-type", "application/zip")

		zw := zip.NewWriter(w)
		defer zw.Close()

		flusher, _ := w.(http.Flusher)

		//result channel will receive the ready images data
		results := make(chan zipItem, len(ids))
		//one rotine per image
		for _, id := range ids {
			go func() {
				results <- s.waitimage(ctx, id)
			}()
		}

		for range len(ids) {
			item := <-results
			id := item.id

			if errors.Is(item.err, context.DeadlineExceeded) {
				log.Printf("timeout waiting for image %s, closing zip", id)
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}

			if item.err != nil {
				log.Printf("image %s skipped: %v", id, item.err)
				continue
			}

			// zw.Flush() does not push the image data out until the next entry.
			f, err := zw.CreateHeader(&zip.FileHeader{Name: id + ".jpeg", Method: zip.Store})
			if err != nil {
				log.Printf("failed to add %s to zip: %v", id, err)
				return
			}
			if _, err := f.Write(item.data); err != nil {
				log.Printf("failed to write %s to zip: %v", id, err)
				return
			}

			// Flush each image to the client as soon as it is written.
			if flusher != nil {
				if err := zw.Flush(); err == nil {
					flusher.Flush()
				}
			}
		}
	}
}

func parseIDs(r *http.Request) []string {
	var ids []string
	for _, id := range strings.Split(r.PathValue("ids"), ",") {
		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

// struct for the outcome of wating for one image in a zip request
type zipItem struct {
	id   string
	data []byte
	err  error
}

func (s *service) waitimage(ctx context.Context, id string) zipItem {
	if regist, ok := s.regis.Get(id); ok {
		select {
		case <-regist.done:
			if regist.err != nil {
				return zipItem{id: id, data: nil, err: regist.err}
			}
		case <-ctx.Done():
			return zipItem{id: id, data: nil, err: ctx.Err()}
		}
	}
	data, ok := s.cache.Get(id)
	s.cache.Add(id, ImageInfo{data.([]byte), time.Now()})
	if !ok {
		return zipItem{id: id, err: errors.New("not found")}
	}

	return zipItem{id: id, data: data.([]byte)}
}
