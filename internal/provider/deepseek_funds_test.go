package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

func TestDeepSeekFunds(t *testing.T) {
	for _, c := range []struct{ name, body, total, funds string }{
		{"grants", `{"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`, "¥110.00", `[{"currency":"CNY","granted":10,"toppedUp":100}]`},
		{"exhausted", `{"balance_infos":[{"currency":"CNY","total_balance":"100","granted_balance":"0","topped_up_balance":100}]}`, "¥100.00", `[{"currency":"CNY","granted":0,"toppedUp":100}]`},
		{"missing", `{"balance_infos":[{"currency":"CNY","total_balance":"100"}]}`, "¥100.00", `[{"currency":"CNY","granted":null,"toppedUp":null}]`},
		{"unreadable", `{"balance_infos":[{"currency":"CNY","total_balance":"100","granted_balance":"NaN","topped_up_balance":"Infinity"}]}`, "¥100.00", `[{"currency":"CNY","granted":null,"toppedUp":null}]`},
		{"independent total", `{"balance_infos":[{"currency":"CNY","total_balance":"110","granted_balance":"9","topped_up_balance":"100"}]}`, "¥110.00", `[{"currency":"CNY","granted":9,"toppedUp":100}]`},
		{"currencies", `{"balance_infos":[{"currency":"CNY","total_balance":"-0.05","granted_balance":"0","topped_up_balance":"-0.05"},{"currency":"USD","total_balance":"11.12","granted_balance":"5"}]}`, "$11.12 · ¥-0.05", `[{"currency":"USD","granted":5,"toppedUp":null},{"currency":"CNY","granted":0,"toppedUp":-0.05}]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			total, funds, err := readDeepSeekFunds([]byte(c.body))
			if err != nil || total != c.total {
				t.Fatalf("total=%q err=%v, want %s", total, err, c.total)
			}
			got, _ := json.Marshal(funds)
			if string(got) != c.funds {
				t.Fatalf("funds=%s, want %s", got, c.funds)
			}
			legacy, err := readDeepSeek([]byte(c.body))
			if err != nil || legacy != total {
				t.Fatalf("legacy balance changed: %q %v", legacy, err)
			}
		})
	}
	for _, body := range []string{`{`, `{"balance_infos":[]}`, `{"balance_infos":[{"currency":"CNY","total_balance":"NaN","granted_balance":"10"}]}`} {
		if _, _, err := readDeepSeekFunds([]byte(body)); err == nil {
			t.Errorf("invalid total accepted: %s", body)
		}
	}
}

// The actual balance endpoint path is carried through the card cache, disk
// fallback, refresh and history. Every request goes to this owned TLS server.
func TestDeepSeekFundsCards(t *testing.T) {
	isolate(t)
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mode atomic.Int32
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/user/balance" && r.URL.Path != "/custom" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if mode.Load() == 2 {
			http.Error(w, "service unavailable", 503)
			return
		}
		if mode.Load() == 3 {
			fmt.Fprint(w, `{"balance_infos":[{"currency":"CNY","total_balance":"108"}]}`)
			return
		}
		grant, total := 10, 110
		if mode.Load() == 1 {
			grant, total = 0, 100
		}
		fmt.Fprintf(w, `{"balance_infos":[{"currency":"CNY","total_balance":"%d","granted_balance":"%d","topped_up_balance":"100"}]}`, total, grant)
	}))
	defer srv.Close()
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.deepseek.com:443" {
			return nil, fmt.Errorf("external address refused: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = tr
	t.Cleanup(func() { http.DefaultClient.Transport = old; tr.CloseIdleConnections(); ForgetBalances() })
	p := Provider{ID: "my-deepseek", Name: "DeepSeek", Key: "fake-test-key", Chat: "https://api.deepseek.com/v1"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	read := func() SubscriptionQuota {
		t.Helper()
		qs := KeyBalances(context.Background())
		if len(qs) != 1 {
			t.Fatalf("cards=%+v", qs)
		}
		return qs[0]
	}
	first := read()
	if first.Balance != "¥110.00" || len(first.BalanceFunds) != 1 || first.BalanceFunds[0].Granted == nil || *first.BalanceFunds[0].Granted != 10 {
		t.Fatalf("breakdown missing on card: %+v", first)
	}
	n := calls.Load()
	if !reflect.DeepEqual(first.BalanceFunds, read().BalanceFunds) || calls.Load() != n {
		t.Fatal("warm cache lost breakdown or reread endpoint")
	}
	// A cold card cache reloads persisted fields after a transient refusal.
	lastQuotas.Lock()
	lastQuotas.m = nil
	lastQuotas.loaded = false
	lastQuotas.Unlock()
	mode.Store(2)
	ForgetBalances()
	stale := read()
	if stale.AsOf == nil || !reflect.DeepEqual(stale.BalanceFunds, first.BalanceFunds) {
		t.Fatalf("stale disk fallback lost breakdown: %+v", stale)
	}
	mode.Store(1)
	ForgetBalances()
	zero := read()
	if zero.AsOf != nil || zero.Balance != "¥100.00" || *zero.BalanceFunds[0].Granted != 0 {
		t.Fatalf("grant exhaustion did not refresh: %+v", zero)
	}
	mode.Store(3)
	ForgetBalances()
	unknown := read()
	if unknown.BalanceFunds[0].Granted != nil || unknown.BalanceFunds[0].ToppedUp != nil || unknown.AsOf != nil {
		t.Fatal("missing fields reused stale amounts or became zero")
	}
	now := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	pts := balancePointsOf(first, now)
	for _, pt := range pts {
		if pt.Amount != 110 {
			t.Fatalf("history double-counted components: %+v", pts)
		}
	}
	if len(pts) != 1 {
		t.Fatalf("breakdown disabled total history: %+v", pts)
	}
	first.User, zero.User = "history-only", "history-only"
	first.ReadAt, zero.ReadAt = nil, nil
	noteBalanceHistory([]SubscriptionQuota{first}, now)
	next := []SubscriptionQuota{zero}
	noteBalanceHistory(next, now.Add(10*time.Minute))
	if next[0].BalanceTrend == nil || len(next[0].BalanceTrend.Points) != 2 {
		t.Fatal("total trend disappeared after grant exhaustion")
	}
	// A user's explicit Balance field wins, even on DeepSeek's host.
	p.BalanceURL = "https://api.deepseek.com/custom"
	p.BalancePath = "¥balance_infos.0.total_balance"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	ForgetBalances()
	custom := read()
	if len(custom.BalanceFunds) != 0 || custom.Balance != "¥108.00" {
		t.Fatalf("custom field acquired grant semantics: %+v", custom)
	}
}
