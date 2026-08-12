package adapters

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// userAgent identifies statisfy to API backends. Several AI-tool backends
// (observed: codebuff.com) silently drop Go's default "Go-http-client/1.1"
// user agent at their WAF, hanging the request until timeout; a real client
// string is answered in ~300ms.
const userAgent = "statisfy/0.1.0 (+https://freebuff.com)"

// httpClient returns a client that speaks HTTP/2 (Go default) with a real
// User-Agent, plus a dialer that races DNS addresses.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: raceDialer(4 * time.Second),
		},
	}
}

// setUA applies the statisfy User-Agent to a request.
func setUA(req *http.Request) {
	req.Header.Set("User-Agent", userAgent)
}

// raceDialer returns a DialContext that resolves the host and races all
// addresses in parallel, returning the first connection that succeeds.
// This avoids stalling on a blackholed first A record (observed: one of
// codebuff.com's two IPs never answers).
func raceDialer(perAttempt time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: perAttempt}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return d.DialContext(ctx, network, addr)
		}
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return d.DialContext(ctx, network, addr)
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, len(ips))
		var wg sync.WaitGroup
		for _, ip := range ips {
			wg.Add(1)
			go func(ip net.IP) {
				defer wg.Done()
				conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				ch <- result{conn, err}
			}(ip)
		}
		go func() {
			wg.Wait()
			close(ch)
		}()

		var firstErr error
		for r := range ch {
			if r.err == nil {
				cancel() // stop the other dials
				return r.conn, nil
			}
			if firstErr == nil {
				firstErr = r.err
			}
		}
		if firstErr == nil {
			firstErr = context.DeadlineExceeded
		}
		return nil, firstErr
	}
}
