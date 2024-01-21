package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"github.com/gamexg/proxyclient"
	"github.com/jellydator/ttlcache/v3"
	"github.com/samber/lo"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"proxy_pool/env"
	"proxy_pool/proxy_manage"
	"strings"
	"time"
)

var logger = log.New(os.Stdout, "proxy server -> ", log.Lshortfile|log.Ldate|log.Ltime)

func sendHTTP(conn net.Conn, statusCode int, contentType string, body string) {
	statusText := fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))

	// 构建响应头部，确保后面有两个CRLF (\r\n\r\n) 表示头部结束
	headers := fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", statusText, contentType, len(body))

	// 将头部和主体写入连接
	fmt.Fprint(conn, headers+body)
}

// ensurePort 检查给定的host字符串，如果没有指定端口，则添加默认端口
func ensurePort(host string, defaultPort string) string {
	_, _, err := net.SplitHostPort(host)

	// 如果出错，说明没有端口，我们添加默认端口
	if err != nil {
		return net.JoinHostPort(host, defaultPort)
	}

	return host
}

// HandleClient deals with client requests and forwards them to the upstream proxy.
func HandleClient(client net.Conn, proxy *ttlcache.Cache[string, string]) {
	defer client.Close()

	reader := bufio.NewReader(client)
	req, err := http.ReadRequest(reader)

	if err != nil {
		return
	}

	// 验证
	proxyAuthHeader := req.Header.Get("Proxy-Authorization")
	if len(proxyAuthHeader) < 6 || strings.ToLower(proxyAuthHeader[:6]) != "basic " {
		sendHTTP(client, http.StatusProxyAuthRequired, "application/json", "Unsupported authorization method")
		return
	}

	proxyAuthDecodeString, err := base64.StdEncoding.DecodeString(proxyAuthHeader[6:])
	proxyAuth := strings.Split(string(proxyAuthDecodeString), ":")
	if err != nil || len(proxyAuth) != 2 {
		sendHTTP(client, http.StatusProxyAuthRequired, "text/plain", "Unsupported authorization method")
		return
	}

	if proxyAuth[1] != "Root@163." {
		sendHTTP(client, http.StatusProxyAuthRequired, "text/plain", "Wrong authorization")
		return
	}

	req.Header.Del("Proxy-Authorization")

	// Connect to the upstream SOCKS5 proxy
	proxyAddr := proxy.Get(proxyAuth[0]).Value()
	logger.Println("use proxy for", proxyAuth[0], proxyAddr, req.URL.Host)
	dialer, err := proxyclient.NewProxyClient(proxyAddr)
	if err != nil {
		logger.Println("use proxy for", "proxy client", err.Error())
		proxy.Delete(proxyAuth[0])
		return
	}
	targetAddr := ensurePort(req.URL.Host, lo.Ternary(req.Method == http.MethodConnect, "443", "80"))
	targetSiteConn, err := dialer.DialTimeout("tcp", targetAddr, time.Second*15)
	if err != nil {
		sendHTTP(client, http.StatusServiceUnavailable, "text/plain", "proxy connect failed")
		logger.Println("use proxy for", "dial", targetAddr, err.Error())
		proxy.Delete(proxyAuth[0])
		return
	}
	defer targetSiteConn.Close()

	// For HTTPS, establish a tunnel
	if req.Method == http.MethodConnect {
		sendHTTP(client, http.StatusOK, "text/plain", "")
	} else {
		req.Write(targetSiteConn)
	}

	// Tunnel the data between client and target site
	go io.Copy(targetSiteConn, client)
	io.Copy(client, targetSiteConn)
}

func main() {
	ln, err := net.Listen("tcp", ":8888")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	manage := proxy_manage.NewProxyManage()
	cacheProxy := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](time.Duration(env.Config.Proxy.CacheSec)*time.Second),
		ttlcache.WithLoader[string, string](ttlcache.LoaderFunc[string, string](
			func(c *ttlcache.Cache[string, string], key string) *ttlcache.Item[string, string] {
				item := c.Set(key, manage.GetProxy(), ttlcache.DefaultTTL)
				return item
			},
		)),
	)
	go cacheProxy.Start()

	for {
		client, err := ln.Accept()
		if err != nil {
			logger.Println(err)
			continue
		}

		go HandleClient(client, cacheProxy)
	}
}
