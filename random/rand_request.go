package randomStr

import (
	"github.com/imroc/req/v3"
	"github.com/samber/lo"
	"proxy_pool/env"
	"regexp"
	"strconv"
)

type funcRule struct {
	match   *regexp.Regexp
	charset []rune
}

var funcRules = []*funcRule{
	{match: regexp.MustCompile(`{rand\((\d+)\)}`), charset: lo.AlphanumericCharset},
	{match: regexp.MustCompile(`{randWord\((\d+)\)}`), charset: lo.LettersCharset},
	{match: regexp.MustCompile(`{randNum\((\d+)\)}`), charset: lo.NumbersCharset},
}

func DoReplace(input string) string {
	for _, rule := range funcRules {
		input = rule.match.ReplaceAllStringFunc(input, func(match string) string {
			matches := rule.match.FindStringSubmatch(match)
			if len(matches) < 2 {
				return match // No replacement if format is incorrect
			}

			length, err := strconv.Atoi(matches[1])
			if err != nil {
				return match // No replacement if length is not a number
			}

			return lo.RandomString(length, rule.charset)
		})
	}

	return input
}

func RandRequest(req *req.Request, config *env.Check) *req.Request {
	req.SetURL(DoReplace(config.ReqUrl))
	req.Method = config.ReqMethod

	if config.ReqBody != nil {
		req.SetBodyBytes([]byte(DoReplace(*config.ReqBody)))
	}

	if config.ReqHead != nil {
		req.Headers = lo.MapEntries(*config.ReqHead, func(key string, value string) (string, []string) {
			return DoReplace(key), []string{DoReplace(value)}
		})
	}

	if config.ReqCookie != nil {
		req.SetHeader("Cookie", DoReplace(*config.ReqCookie))
	}

	return req
}
