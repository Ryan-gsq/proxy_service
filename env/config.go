package env

import (
	"encoding/json"
	"github.com/nasdf/ulimit"
	"github.com/samber/lo"
	"github.com/spf13/viper"
	"io"
	"os"
	"regexp"
)

type Check struct {
	ReqUrl    string             `json:"reqUrl"`
	ReqMethod string             `json:"reqMethod"`
	ReqBody   *string            `json:"reqBody"`
	ReqHead   *map[string]string `json:"reqHead"`
	ReqCookie *string            `json:"reqCookie"`

	RspCode      int    `json:"rspCode"`
	RspBodyRe    string `json:"rspBodyRe"`
	RspReverseRe bool   `json:"rspReverseRe"`
	RspBodyRe_   *regexp.Regexp
}

type Proxy struct {
	Timeout  int `json:"timeout"`
	CacheSec int `json:"cacheSec"`
	MaxPool  int `json:"maxPool"`

	Checks []*Check `json:"checks"`
}

type MainConfig struct {
	Proxy         *Proxy `json:"proxy"`
	ProxyRuleList []map[string]string
	init          bool
}

var Config = new(MainConfig)

func init() {
	viper.SetConfigType("yaml")     // REQUIRED if the env file does not have the extension in the name
	viper.AddConfigPath("./assets") // path to look for the env file in

	viper.SetConfigName("env")
	lo.Must0(viper.MergeInConfig())

	lo.Must0(viper.Unmarshal(Config))
	Config.init = true

	// Increase ulimit
	lo.Must0(ulimit.SetRlimit(uint64(1000000)))

	open := lo.Must(os.Open("./assets/proxy_list.json"))
	defer open.Close()
	json.Unmarshal(lo.Must(io.ReadAll(open)), &Config.ProxyRuleList)
	lo.Shuffle(Config.ProxyRuleList)
}
