package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/AvalonWot/filesrv/log"
	"go.uber.org/zap"

	"github.com/cavaliercoder/grab"
)

var _cfg *config
var _mgr = DownloadFileTaskMgr{}

var _resolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{
			Timeout: time.Millisecond * time.Duration(10000),
		}
		return d.DialContext(ctx, network, "192.168.1.1:53")
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
		log.Info("创建下载任务", zap.String("path", dst), zap.String("url", urlStr))
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
		defer func() {
			d.done <- struct{}{}
		}()
		file, err := d.downlaodFile()
		if err != nil {
			return
		}
		path, _ := filepath.Split(dst)
		log.Info("创建目录", zap.String("path", path))
		if err := os.MkdirAll(path, os.ModeDir|0755); err != nil {
			log.Error("创建目录失败", zap.String("path", path), zap.Error(err))
			os.Exit(1)
		}
		log.Info("移动文件", zap.String("file", file), zap.String("dst", dst))
		if err := Move(file, dst); err != nil {
			log.Error("移动下载文件失败", zap.String("path", path), zap.String("dst", dst), zap.Error(err))
			os.Exit(1)
		}
	}()
	return d
}

func (d *DownloadFileTask) downlaodFile() (string, error) {
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
	req, err := grab.NewRequest(".", d.Url)
	if err != nil {
		log.Error("创建下载任务错误", zap.Error(err))
		return "", errors.WithMessage(err, "创建下载任务错误")
	}

	// start download
	log.Info("Downloading", zap.String("url", req.URL().String()))
	resp := client.Do(req)
	// log.Info("下载返回的状态码", zap.String("status", resp.HTTPResponse.Status))

	// start UI loop
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			log.Info("下载进度",
				zap.String("url", req.URL().String()),
				zap.Int64("completed", resp.BytesComplete()),
				zap.Int64("all_size", resp.Size),
				zap.Float64("progress", 100*resp.Progress()))

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	if err := resp.Err(); err != nil {
		log.Error("下载发生错误", zap.Error(err))
		return "", errors.WithMessage(err, "下载发生错误")
	}

	log.Info("下载文件完成", zap.String("url", d.Url), zap.String("file_name", resp.Filename))
	return resp.Filename, nil
}

type CacheFileHanlder struct {
	root string
}

func getOriginUrl(r *http.Request) string {
	return fmt.Sprintf("http://%s%s", r.Host, r.URL.Path)
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
		originUrl := getOriginUrl(r)
		log.Info("文件请求", zap.String("remote", r.RemoteAddr), zap.String("fullname", fullName), zap.String("origin_url", originUrl))
		if _, err := os.Stat(fullName); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_mgr.CreateDownladTask(fullName, originUrl)
				http.Error(w, "wait for downloading", 404)
			} else {
				fmt.Printf("[ERR] unknonw err: %v\n", err)
				http.Error(w, "unknow err", 500)
			}
			return
		} else {
			http.ServeFile(w, r, fullName)
		}
	} else {
		http.Error(w, "invalid method", 500)
	}
}

func NewCacheFileHanlder(root string) *CacheFileHanlder {
	return &CacheFileHanlder{root: root}
}

var cfgPath = flag.String("cfg", "", "config path")

func main() {
	flag.Parse()
	cfg, err := ParseConfig(*cfgPath)
	if err != nil {
		fmt.Printf("解析配置文件发生错误: %v", err)
		return
	}
	_cfg = cfg
	log.InitLog(_cfg.LogPath, _cfg.Verbose)

	if err := http.ListenAndServe(_cfg.Listen, NewCacheFileHanlder(_cfg.FilesPath)); err != nil {
		fmt.Printf("启动http服务错误: %v", err)
		return
	}
}

func Move(source, destination string) error {
	err := os.Rename(source, destination)
	if err != nil && strings.Contains(err.Error(), "invalid cross-device link") {
		return moveCrossDevice(source, destination)
	}
	return err
}

func moveCrossDevice(source, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return errors.Wrap(err, "Open(source)")
	}
	dst, err := os.Create(destination)
	if err != nil {
		src.Close()
		return errors.Wrap(err, "Create(destination)")
	}
	_, err = io.Copy(dst, src)
	src.Close()
	dst.Close()
	if err != nil {
		return errors.Wrap(err, "Copy")
	}
	fi, err := os.Stat(source)
	if err != nil {
		os.Remove(destination)
		return errors.Wrap(err, "Stat")
	}
	err = os.Chmod(destination, fi.Mode())
	if err != nil {
		os.Remove(destination)
		return errors.Wrap(err, "Stat")
	}
	os.Remove(source)
	return nil
}
