package core

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/domac/kapok/util"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	USER_AGENT = "kapok"
)

type Worker struct {
	duration    int
	concurrecy  int
	testUrl     string
	header      string
	method      string
	statsChann  chan *Stats
	timeoutms   int
	compress    bool
	keepAlive   bool
	interrupted int32
	bodyReadedr []byte
}

func NewWorker(
	testUrl string,
	concurrecy int,
	duration int,
	timeout int,
	header string,
	method string,
	statsChann chan *Stats,
	disableka bool,
	co bool, bodyReadedr []byte) (worker *Worker) {
	worker = &Worker{duration, concurrecy, testUrl, header,
		method, statsChann, timeout, co, disableka, 0, bodyReadedr}
	return
}

//HTTP请求
func DoRequest(httpClient *http.Client, headers map[string]string, method, loadUrl string, bodydata []byte) (respSize int, num2x int, num5x int, duration time.Duration) {
	respSize = -1
	duration = -1
	num5x = 0
	num2x = 0
	loadUrl = util.EscapeUrlStr(loadUrl)

	req, err := http.NewRequest(method, loadUrl, bytes.NewBuffer(bodydata))
	if err != nil {
		fmt.Println("An error occured doing request", err)
		return
	}

	req.Header.Add("User-Agent", USER_AGENT)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		//fmt.Println("redirect error")
		rr, ok := err.(*url.Error)
		if !ok {
			fmt.Println("An error occured doing request", err, rr)
			return
		}
	}
	if resp == nil {
		//fmt.Println("empty response error")
		return
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("An error occured reading body", err)
	}
	if resp.StatusCode == http.StatusOK {
		duration = time.Since(start)
		respSize = len(body) + int(util.EstimateHttpHeadersSize(resp.Header))
		num2x += 1
	} else if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect {
		duration = time.Since(start)
		respSize = int(resp.ContentLength) + int(util.EstimateHttpHeadersSize(resp.Header))
	} else if resp.StatusCode >= 500 {
		num5x += 1
	} else if resp.StatusCode == http.StatusMethodNotAllowed {
		duration = time.Since(start)
		respSize = len(body) + int(util.EstimateHttpHeadersSize(resp.Header))
		num2x += 1
	} else {
		//fmt.Println("received status code", resp.StatusCode, "from", resp.Header, "content", string(body), req)
	}
	return
}

func (w *Worker) RunSingleNode() {
	stats := &Stats{MinRequestTime: time.Minute}

	start := time.Now()
	httpClient := &http.Client{}

	pUrl, _ := url.Parse(w.testUrl)
	var tlsConfig *tls.Config
	if pUrl.Scheme == "https" {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else {
		tlsConfig = nil
	}

	httpClient.Transport = &http.Transport{
		DisableCompression:    w.compress,
		DisableKeepAlives:     w.keepAlive,
		ResponseHeaderTimeout: time.Millisecond * time.Duration(w.timeoutms),
		TLSClientConfig:       tlsConfig,
	}

	httpClient.Timeout = time.Second * time.Duration(w.duration)

	//增加对headers的处理
	sets := strings.Split(w.header, ";")
	headerMap := make(map[string]string)
	for i := range sets {
		split := strings.SplitN(sets[i], ":", 2)
		if len(split) == 2 {
			headerMap[split[0]] = split[1]
		}
	}

	//持续间隔
	for time.Since(start).Seconds() <= float64(w.duration) && atomic.LoadInt32(&w.interrupted) == 0 {
		respSize, num2x, num5x, reqDur := DoRequest(httpClient, headerMap, w.method, w.testUrl, w.bodyReadedr)
		if respSize > 0 {
			stats.RespSize += int64(respSize)
			stats.Duration += reqDur
			stats.MaxRequestTime = util.MaxDuration(reqDur, stats.MaxRequestTime)
			stats.MinRequestTime = util.MinDuration(reqDur, stats.MinRequestTime)
			stats.NumRequests++
			stats.Num2X += num2x
		} else {
			stats.Num5X += num5x
			stats.NumErrs++
		}
	}
	w.statsChann <- stats
}

func (w *Worker) Stop() {
	atomic.StoreInt32(&w.interrupted, 1)
}
