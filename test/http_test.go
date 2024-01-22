package test

import (
	"fmt"
	"github.com/gamexg/proxyclient"
	"io"
	"testing"
	"time"
)

func TestHttp(t *testing.T) {
	p, err := proxyclient.NewProxyClient("http://127.0.0.1:7890")
	if err != nil {
		panic(err)
	}

	c, err := p.Dial("tcp", "www.163.com:443")
	if err != nil {
		panic(err)
	}

	io.WriteString(c, "GET / HTTP/1.1\r\nHOST:www.163.com\r\n\r\n")
	io.WriteString(c, "GET / HTTP/1.1\r\nHOST:www.163.com\r\n\r\n")

	go func() {
		for {
			bt := make([]byte, 1024)
			b, err := c.Read(bt)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bt[:b]))
		}
	}()
	time.Sleep(10 * time.Second)

}
