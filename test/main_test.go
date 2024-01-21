package test

import (
	"fmt"
	"proxy_pool/proxy_manage"
	"testing"
	"time"
)

func TestA(t *testing.T) {
	manage := proxy_manage.NewProxyManage()
	pm := make(map[string]interface{})
	proxyList := make([]string, 0)

	go func() {
		for {
			time.Sleep(time.Second)
			fmt.Printf("%d\t%d\n", len(pm), len(proxyList))
		}
	}()

	for {
		proxy := manage.GetProxy()
		pm[proxy] = nil
		proxyList = append(proxyList, proxy)
	}
}
