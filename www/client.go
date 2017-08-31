package main

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
)

func main() {
	client := &http.Client{}

	buf := []byte(`"id": "100"`)
	r := bytes.NewReader(buf)

	req, err := http.NewRequest("POST", "http://127.0.0.1:8000", r)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	spew.Dump(resp.Header["Set-Cookie"])
	s := strings.Split(resp.Header["Set-Cookie"][0], ";")
	ss := strings.SplitN(s[0], "=", 2)
	spew.Dump(ss)

	fmt.Printf("===============\n")
	// lift csrf cookie
	r2 := bytes.NewReader(buf)
	req2, err := http.NewRequest("POST", "http://127.0.0.1:8000", r2)
	req2.Header.Add("X-CSRF-Token", ss[1])
	expiration := time.Now().Add(365 * 24 * time.Hour)
	cookie := http.Cookie{Name: "csrf_token", Value: ss[1], Expires: expiration}
	req2.AddCookie(&cookie)
	spew.Dump(req2.Header)
	resp2, err := client.Do(req2)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	spew.Dump(resp2.Body)
}
