package main

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"sync"

	"github.com/alecthomas/kingpin/v2"
	"github.com/coreos/go-systemd/v22/login1"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

type data struct {
	lock    sync.Mutex
	headers map[string]map[string]interface{}
	values  map[string][]string
	keys    []string
}

var re1 = regexp.MustCompile(`^\s*#\s*(?:HELP|TYPE)\s*(\S+)`)
var re2 = regexp.MustCompile(`^\s*([^{\s]+)`)
var re3 = []byte("name=")

func readOnce(d *data, user login1.User, addr string) error {
	exp, err := net.Dial("unix", addr)
	if err != nil {
		return err
	}
	defer exp.Close()
	if user.Name != "" {
		_, err = exp.Write([]byte("GET /metrics HTTP/1.1\r\nHost: " + user.Name + "\r\nConnection: close\r\n\r\n"))
	} else {
		_, err = exp.Write([]byte("GET /metrics HTTP/1.1\r\nHost: system\r\nConnection: close\r\n\r\n"))
	}
	if err != nil {
		return err
	}
	res, err := http.ReadResponse(bufio.NewReader(exp), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	re4 := []byte("user=\"" + user.Name + "\",name=")
	scanner := bufio.NewScanner(res.Body)
	d.lock.Lock()
	defer d.lock.Unlock()
	for scanner.Scan() {
		data := scanner.Bytes()
		if user.Name != "" {
			data = bytes.Replace(data, re3, re4, -1)
		}
		if matches := re1.FindSubmatch(data); matches != nil {
			list, ok := d.headers[string(matches[1])]
			if !ok {
				list = make(map[string]interface{})
				d.headers[string(matches[1])] = list
			}
			d.headers[string(matches[1])][string(data)] = true
		} else if matches := re2.FindSubmatch(data); matches != nil {
			list, ok := d.values[string(matches[1])]
			if !ok {
				list = []string{}
				d.keys = append(d.keys, string(matches[1]))
			}
			d.values[string(matches[1])] = append(list, string(data))
		}
	}
	return scanner.Err()
}

type server struct{}

func (_ *server) ServeHTTP(wr http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/metrics" || req.Method != "GET" {
		wr.WriteHeader(404)
		return
	}
	conn, err := login1.New()
	defer conn.Close()
	if err != nil {
		log.Println(err)
		wr.WriteHeader(500)
		return
	}
	users, err := conn.ListUsersContext(context.TODO())
	if err != nil {
		log.Println(err)
		wr.WriteHeader(500)
		return
	}
	d := data{
		headers: make(map[string]map[string]interface{}),
		values:  make(map[string][]string),
	}
	var wg sync.WaitGroup
	wg.Add(len(users) + 1)
	go func() {
		defer wg.Done()
		err = readOnce(&d, login1.User{}, "/tmp/systemd_exporter/systemd_exporter.sock")
		failed := "0"
		if err != nil {
			failed = "1"
		}
		d.lock.Lock()
		defer d.lock.Unlock()
		list, ok := d.values["systemd_system_failed"]
		if !ok {
			list = []string{}
			d.keys = append(d.keys, "systemd_system_failed")
		}
		d.values["systemd_system_failed"] = append(list, "systemd_system_failed "+failed)
		if err != nil {
			log.Println(err)
			return
		}
	}()
	for _, user := range users {
		go func(user login1.User) {
			defer wg.Done()
			rp, err := conn.GetUserPropertyContext(context.TODO(), user.Path, "RuntimePath")
			if err == nil {
				err = readOnce(&d, user, rp.Value().(string)+"/systemd_exporter.sock")
			}
			failed := "0"
			if err != nil {
				failed = "1"
			}
			d.lock.Lock()
			defer d.lock.Unlock()
			list, ok := d.values["systemd_user_failed"]
			if !ok {
				list = []string{}
				d.keys = append(d.keys, "systemd_user_failed")
			}
			d.values["systemd_user_failed"] = append(list, "systemd_user_failed{user=\""+user.Name+"\"} "+failed)
			if err != nil {
				log.Println(err)
				return
			}
		}(user)
	}
	wg.Wait()
	sort.Slice(d.keys, func(i, j int) bool { return d.keys[i] < d.keys[j] })
	wr.Header().Set("Content-Type", "text/plain; charset=utf-8")
	wr.WriteHeader(200)
	for _, k := range d.keys {
		h, ok := d.headers[k]
		if ok {
			for hh := range h {
				_, err := wr.Write([]byte(hh + "\n"))
				if err != nil {
					log.Println(err)
					return
				}
			}
		}
		for _, ll := range d.values[k] {
			_, err := wr.Write([]byte(ll + "\n"))
			if err != nil {
				log.Println(err)
				return
			}
		}
	}
}

func main() {
	var (
		metricsPath = kingpin.Flag(
			"web.telemetry-path",
			"Path under which to expose metrics.",
		).Default("/metrics").String()
		toolkitFlags = webflag.AddFlags(kingpin.CommandLine, ":9558")
	)

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("systemd_user_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting systemd_user_exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "build_context", version.BuildContext())

	http.Handle(*metricsPath, &server{})

	srv := &http.Server{}
	if err := web.ListenAndServe(srv, toolkitFlags, logger); err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}
}
