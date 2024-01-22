package proxy_manage

import (
	"errors"
	"fmt"
	"github.com/imroc/req/v3"
	"github.com/samber/lo"
	"github.com/samber/lo/parallel"
	"log"
	"os"
	"proxy_pool/env"
	randomStr "proxy_pool/random"
	"regexp"
	"strings"
	"sync"
	"time"
)

var logger = log.New(os.Stdout, "proxy manage -> ", log.Lshortfile|log.Ldate|log.Ltime)

type ProxyManage struct {
	proxyQueryChan       chan []string
	proxyUpdateQueryChan chan []string
	proxyChan            chan string

	proxyCacheSet map[string]*interface{}
	outerIp       string
	lock          sync.Mutex
}

func NewProxyManage() *ProxyManage {
	p := &ProxyManage{
		proxyQueryChan:       make(chan []string, lo.Clamp(env.Config.Proxy.MaxPool/50, 1, 1000)),
		proxyUpdateQueryChan: make(chan []string, lo.Clamp(env.Config.Proxy.MaxPool/50, 1, 1000)),
		proxyChan:            make(chan string, env.Config.Proxy.MaxPool),
		proxyCacheSet:        make(map[string]*interface{}),
		lock:                 sync.Mutex{},
	}

	p.initProxyQueue()

	return p
}

func (self *ProxyManage) createClient(timeOutSec, maxConns int) *req.Client {
	timeout := time.Duration(timeOutSec) * time.Second
	client := req.C()
	client.SetTimeout(timeout).SetIdleConnTimeout(timeout).SetResponseHeaderTimeout(timeout).SetTLSHandshakeTimeout(timeout)
	client.DisableCompression().EnableInsecureSkipVerify().SetMaxConnsPerHost(maxConns)
	client.SetCommonRetryCount(0).SetCookieJar(nil).DisableKeepAlives()

	client.Headers = map[string][]string{
		"Accept":          {"text/plain,application/json"},
		"Accept-Language": {"ja,en-US;q=0.9,en;q=0.8"},
		"Cache-Control":   {"no-cache"},
		"Connection":      {"close"},
		"Pragma":          {"no-cache"},
		"User-Agent":      {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"},
	}
	return client
}

func (self *ProxyManage) initProxyQueue() {
	// 更新代理检查规则
	self.updateCheckRuleRe()
	go func() {
		for {
			time.Sleep(time.Minute)
			self.updateCheckRuleRe()
		}
	}()

	// 从规则获取代理列表
	go func() {
		client := self.createClient(5, 1)
		for {
			self.lock.Lock()
			self.proxyCacheSet = make(map[string]*interface{})
			self.lock.Unlock()
			for _, rule := range env.Config.ProxyRuleList {
				lo.Try0(func() {
					do := client.Get(rule["url"]).Do()
					if do.IsSuccessState() {
						ps := lo.Map(strings.Split(strings.ReplaceAll(lo.Must(do.ToString()), "\r", ""), "\n"), func(item string, index int) string {
							return fmt.Sprintf("%s://%s", rule["type"], item)
						})

						lo.ForEach(lo.Chunk(ps, 500), func(items []string, _ int) {
							self.proxyQueryChan <- lo.Map(items, func(item string, index int) string {
								return strings.Trim(item, " ")
							})
						})
					} else {
						logger.Printf("failed get proxy rule : %v\n", rule)
					}
				})
			}
		}
	}()

	// 代理检查
	go func() {
		go func() {
			rateCh := make(chan any, cap(self.proxyQueryChan))
			for proxyQuery := range self.proxyQueryChan {
				rateCh <- nil
				go func(proxyQuery []string) {
					checkAbleProxy := self.checkAbleProxy(proxyQuery, false)
					self.applyCheckedProxy(checkAbleProxy)
					<-rateCh
				}(proxyQuery)
			}
		}()
		go func() {
			rateCh := make(chan any, cap(self.proxyQueryChan))
			for proxyQuery := range self.proxyUpdateQueryChan {
				rateCh <- nil
				go func(proxyQuery []string) {
					checkAbleProxy := self.checkAbleProxy(proxyQuery, true)
					self.applyCheckedProxy(checkAbleProxy)
					<-rateCh
				}(proxyQuery)
			}
		}()
	}()

	// 检查现有代理连通性
	go func() {
		for {
			time.Sleep(time.Minute)
			for _, proxies := range lo.Chunk(lo.ChannelToSlice(self.proxyChan), 50) {
				self.proxyUpdateQueryChan <- proxies
			}
		}
	}()
}

func (self *ProxyManage) updateCheckRuleRe() {
	c := self.createClient(env.Config.Proxy.Timeout, 1)
	if ip, err := c.Get("https://api.ipify.org").Do().ToString(); err != nil {
		logger.Println("failed to get local ip address", err.Error())
		self.outerIp = "127.0.0.1"
	} else {
		self.outerIp = ip
	}
	self.lock.Lock()
	lo.ForEach(env.Config.Proxy.Checks, func(item *env.Check, index int) {
		if item.RspBodyRe == "{ip}" {
			item.RspBodyRe_ = regexp.MustCompile(regexp.QuoteMeta(self.outerIp))
		} else {
			item.RspBodyRe_ = regexp.MustCompile(item.RspBodyRe)
		}
	})
	self.lock.Unlock()
}

func (self *ProxyManage) applyCheckedProxy(checkAbleProxy []lo.Tuple2[string, bool]) {
	for _, checked := range checkAbleProxy {
		if checked.B {
			self.lock.Lock()
			if _, has := self.proxyCacheSet[checked.A]; has {
				self.lock.Unlock()
				continue
			}
			self.proxyCacheSet[checked.A] = nil
			self.lock.Unlock()
			self.proxyChan <- checked.A
		}
	}
}

func (self *ProxyManage) checkAbleProxy(proxyQuery []string, forceCheck bool) []lo.Tuple2[string, bool] {
	proxyConfig := env.Config.Proxy

	return parallel.Map(proxyQuery, func(pl string, _ int) lo.Tuple2[string, bool] {
		if !forceCheck {
			self.lock.Lock()
			if _, ok := self.proxyCacheSet[pl]; ok {
				self.lock.Unlock()
				return lo.T2(pl, true)
			}
			self.lock.Unlock()
		}

		c := self.createClient(proxyConfig.Timeout, len(proxyConfig.Checks))
		c.SetProxyURL(pl)

		if lo.Count(parallel.Map(proxyConfig.Checks, func(check *env.Check, index int) bool {
			return lo.Try0(func() {
				rsp := randomStr.RandRequest(c.R(), check).Do()
				if rsp.GetStatusCode() != check.RspCode {
					match := check.RspBodyRe_.Match(lo.Must(rsp.ToBytes()))
					if (check.RspReverseRe && match) || !match {
						panic(errors.New("not much"))
					}
				}
			})
		}), true) == len(proxyConfig.Checks) {
			return lo.T2(pl, true)
		} else {
			return lo.T2(pl, false)
		}
	})
}

func (self *ProxyManage) GetProxy() string {
	return <-self.proxyChan
}

func (self *ProxyManage) GetLen() int {
	return len(self.proxyChan)
}
