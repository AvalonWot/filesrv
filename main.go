package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cavaliercoder/grab"
)

var _mgr = DownloadFileTaskMgr{}

var _resolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{
			Timeout: time.Millisecond * time.Duration(10000),
		}
		return d.DialContext(ctx, network, "114.114.114.114:53")
	},
}

type DownloadFileTaskMgr struct {
	lock  sync.Mutex
	tasks map[string]DownloadFileTask
}

func (mgr *DownloadFileTaskMgr) CreateDownladTask(dst, urlStr string) {
	mgr.lock.Lock()
	defer mgr.lock.Unlock()
	if mgr.tasks == nil {
		mgr.tasks = make(map[string]DownloadFileTask, 100)
	}
	if _, ok := mgr.tasks[urlStr]; !ok {
		fmt.Printf("create download task: %s\n", urlStr)
		task := createDownloadFileTask(dst, urlStr)
		mgr.tasks[urlStr] = task
		go func() {
			<-task.done
			mgr.lock.Lock()
			defer mgr.lock.Unlock()
			delete(mgr.tasks, urlStr)
		}()
	}
}

type DownloadFileTask struct {
	Url  string
	done chan struct{}
}

func createDownloadFileTask(dst, urlStr string) DownloadFileTask {
	d := DownloadFileTask{
		Url:  urlStr,
		done: make(chan struct{}),
	}
	go func() {
		file := d.downlaodFile()
		path, _ := filepath.Split(dst)
		fmt.Printf("create dir: %s\n", path)
		if err := os.MkdirAll(path, os.ModeDir|0755); err != nil {
			fmt.Fprintf(os.Stderr, "Create Dir Fail: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("mv file %s to %s\n", file, dst)
		if err := os.Rename(file, dst); err != nil {
			fmt.Fprintf(os.Stderr, "move download file fail: %v\n", err)
			os.Exit(1)
		}
		d.done <- struct{}{}
	}()
	return d
}

func (d *DownloadFileTask) downlaodFile() string {
	// 使用外部的dns进行解析, 避免被本地的路由配置的dns劫持弄成回环
	dialer := &net.Dialer{
		Timeout:  15 * time.Second,
		Resolver: _resolver,
	}
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	client := &grab.Client{
		UserAgent: "grab",
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: dialContext,
			},
		},
	}
	req, _ := grab.NewRequest(".", d.Url)

	// start download
	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	fmt.Printf("  %v\n", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("%s\n  transferred %v / %v bytes (%.2f%%)\n",
				req.URL(),
				resp.BytesComplete(),
				resp.Size,
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Download saved to ./%v \n", resp.Filename)
	return resp.Filename
}

type CacheFileHanlder struct {
	root string
}

func getOriginUrl(r *http.Request) string {
	return fmt.Sprintf("http://%s%s", r.Host, r.URL.String())
}

func isSlashRune(r rune) bool { return r == '/' || r == '\\' }

func containsDotDot(v string) bool {
	if !strings.Contains(v, "..") {
		return false
	}
	for _, ent := range strings.FieldsFunc(v, isSlashRune) {
		if ent == ".." {
			return true
		}
	}
	return false
}

func (h *CacheFileHanlder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if containsDotDot(r.URL.Path) {
			http.Error(w, "invaild path", 400)
			return
		}
		fullName := filepath.Join(h.root, filepath.FromSlash(path.Clean(r.URL.Path)))
		fmt.Printf("fullname: %s\n", fullName)
		if _, err := os.Stat(fullName); err == nil {
			http.ServeFile(w, r, fullName)
		} else {
			if errors.Is(err, os.ErrNotExist) {
				_mgr.CreateDownladTask(fullName, getOriginUrl(r))
				http.Error(w, "wait for downloading", 404)
			} else {
				fmt.Printf("[ERR] unknonw err: %v\n", err)
				http.Error(w, "unknow err", 500)
			}
		}
	} else {
		http.Error(w, "invalid method", 500)
	}
}

func NewCacheFileHanlder(root string) *CacheFileHanlder {
	return &CacheFileHanlder{root: root}
}

func main() {
	log.Fatal(http.ListenAndServe(":80", NewCacheFileHanlder("./")))
}
