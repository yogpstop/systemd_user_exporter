package main

import (
	"bufio"
	"bytes"
	"context"
	"github.com/coreos/go-systemd/v22/login1"
	"log"
	"net"
	"net/http"
	"regexp"
	"sort"
)

type data struct {
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
	_, err = exp.Write([]byte("GET /metrics HTTP/1.1\r\nHost: " + user.Name + "\r\nConnection: close\r\n\r\n"))
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
	for scanner.Scan() {
		data := bytes.Replace(scanner.Bytes(), re3, re4, -1)
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
		make(map[string]map[string]interface{}),
		make(map[string][]string),
		[]string{},
	}
	for _, user := range users {
		rp, err := conn.GetUserPropertyContext(context.TODO(), user.Path, "RuntimePath")
		if err != nil {
			log.Println(err)
			continue
		}
		err = readOnce(&d, user, rp.Value().(string)+"/systemd_exporter.sock")
		if err != nil {
			log.Println(err)
			continue
		}
	}
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
	srv := &http.Server{Handler: &server{}}
	ln, err := net.Listen("tcp", ":9558")
	if err != nil {
		panic(err)
	}
	err = srv.Serve(ln)
	if err != nil {
		log.Println(err)
	}
}
