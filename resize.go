package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"

	jpgresize "github.com/nfnt/resize"
)

var fetchClient = &http.Client{Timeout: 10 * time.Second}

func (s *service) processResizes(ctx context.Context, request resizeRequest) ([]resizeResult, error) {
	results := make([]resizeResult, 0, len(request.URLs))
	for _, url := range request.URLs {
		result := resizeResult{}
		id := genID(url, request.Width, request.Height)
		newURL := proto + hostport + "/v1/image/" + id + "/image.jpeg"

		if s.cache.Contains(id) {
			result.ID = id
			result.URL = newURL
			result.Result = success
			result.Cached = true
			results = append(results, result)
			continue
		}

		e, exist := s.regis.Start(id)
		if exist {
			select {
			case <-e.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			if e.err != nil {
				result.Result = failure
			} else {
				result.ID = id
				result.URL = newURL
				result.Result = success
			}
			results = append(results, result)
			continue
		}

		data, err := fetchAndResize(ctx, url, request.Width, request.Height)
		if err != nil {
			log.Printf("failed to resize %s: %v", url, err)
			s.regis.Finish(id, err)
			result.Result = failure
			results = append(results, result)
			continue
		}

		log.Print("caching ", id)
		s.cache.Add(id, ImageInfo{
			Img: data,
			LastUsed: time.Now(),
		}) //LRU.cache already handles the lock and unlock
		s.regis.Finish(id, nil)

		result.ID = id
		result.URL = newURL
		result.Result = success
		result.Cached = false
		results = append(results, result)
	}

	return results, nil
}

func (s *service) processResizesAsync(ctx context.Context, request resizeRequest) ([]resizeResult, error) {
	results := make([]resizeResult, 0, len(request.URLs))
	for _, url := range request.URLs {
		result := resizeResult{}
		id := genID(url, request.Width, request.Height)
		newURL := proto + hostport + "/v1/image/" + id + "/image.jpeg"

		if s.cache.Contains(id) {
			result.ID = id
			result.URL = newURL
			result.Result = success
			result.Cached = true
			results = append(results, result)
			continue
		}

		j := job{
			ID:     id,
			URL:    url,
			Width:  request.Width,
			Height: request.Height,
			Ctx: ctx,
		}

		accepted := true
		result.Result = success
		result.Cached = false

		_, exist := s.regis.Start(id)
		//submit for worker
		if !exist {
			accepted = s.wp.Submit(j)
		}
		if !accepted { //buffer is full
			result.Result = failure
			log.Printf("buffer full, rejecting %s", url)
			s.regis.Finish(id, errors.New("buffer full"))
			results = append(results, result)
			continue
		}

		result.ID = id
		result.URL = newURL

		results = append(results, result)
	}
	return results, nil
}

func fetchAndResize(ctx context.Context, url string, width uint, height uint) ([]byte, error) {
	data, err := fetch(ctx, url)
	if err != nil {
		return nil, err
	}

	return resize(data, width, height)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	log.Print("fetching ", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err!=nil{
		return nil, fmt.Errorf("failed to create request to fetch: %v", err)
	}
	r, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %v", err)
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 status: %d", r.StatusCode)
	}

	data, err := ioutil.ReadAll(io.LimitReader(r.Body, 15*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read fetch data: %v", err)
	}

	return data, nil
}

func resize(data []byte, width uint, height uint) ([]byte, error) {
	// decode jpeg into image.Image
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to jped decode: %v", err)
	}

	var newImage image.Image

	// if either width or height is 0, it will resize respecting the aspect ratio
	newImage = jpgresize.Resize(width, height, img, jpgresize.Lanczos3)

	newData := bytes.Buffer{}
	err = jpeg.Encode(bufio.NewWriter(&newData), newImage, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to jpeg encode resized image: %v", err)
	}

	return newData.Bytes(), nil
}

func genID(url string, width, height uint) string {
	widthStr := strconv.FormatUint(uint64(width), 10)
	heightStr := strconv.FormatUint(uint64(height), 10)
	value := fmt.Appendf(nil, "%s|%sx%s", url, widthStr, heightStr)
	hash := sha256.Sum256(value)
	return base64.URLEncoding.EncodeToString(hash[:])
}

