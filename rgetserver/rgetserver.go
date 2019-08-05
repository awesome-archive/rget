package rgetserver

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/merklecounty/rget/gitcache"
	"github.com/merklecounty/rget/rgethash"
	"github.com/merklecounty/rget/rgetwellknown"
)

type Server struct {
	*gitcache.GitCache
	ProjReqs *prometheus.CounterVec
}

func (r Server) ReleaseHandler(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		http.Error(resp, "only GET is supported", http.StatusBadRequest)
		return
	}

	short, err := rgetwellknown.TrimDigestDomain(req.Host)
	if err != nil {
		fmt.Printf("request for unknown host %v unable to parse: %v\n", req.Host, err)
	}
	if len(short) > 0 {
		r.ProjReqs.WithLabelValues(req.Method, short).Inc()
	}

	full := strings.TrimSuffix(req.Host, "."+rgetwellknown.PublicServiceHost)
	fmt.Fprintf(resp, "<h2>%s</h2>", short)
	fmt.Fprintf(resp, "<a href=\"https://github.com/merklecounty/records/blob/master/%s\">Merkle County Record</a>", full)

	return
}

func (r Server) APIHandler(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(resp, "only POST is supported", http.StatusBadRequest)
		return
	}

	err := req.ParseForm()
	if err != nil {
		http.Error(resp, "invalid request", http.StatusBadRequest)
		return
	}

	sumsURL := req.Form.Get("url")
	fmt.Printf("submission: %v\n", sumsURL)

	// ensure the URL is coming from a host we know how to generate a
	// domain for by parsing it using the wellknown libraries
	domain, err := rgetwellknown.Domain(sumsURL)
	if err != nil {
		fmt.Printf("wellknown domain error: %v\n", err)
		resp.WriteHeader(http.StatusOK)
		return
	}

	r.ProjReqs.WithLabelValues(req.Method, domain).Inc()

	// Step 1: Download the SHA256SUMS that is correct for the URL
	response, err := http.Get(sumsURL)
	var sha256file []byte
	if err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	} else {
		var err error
		defer response.Body.Close()
		sha256file, err = ioutil.ReadAll(response.Body)
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}
	}

	sums := rgethash.FromSHA256SumFile(string(sha256file))

	// Step 2: Save the file contents to the git repo by domain
	_, err = r.GitCache.Get(context.Background(), sums.Domain())
	if err == nil {
		// TODO(philips): add rate limiting and DDoS protections here
		fmt.Printf("cache hit: %v\n", sumsURL)
		resp.WriteHeader(http.StatusOK)
		return
	}

	// Step 3. Create the Certificate object for the domain and save that as well
	ctdomain := sums.Domain() + "." + domain
	err = r.GitCache.Put(context.Background(), ctdomain, sha256file)
	if err != nil {
		fmt.Printf("git put error: %v", err)
		http.Error(resp, "internal service error", http.StatusInternalServerError)
		return
	}

	resp.WriteHeader(http.StatusOK)
	return
}
