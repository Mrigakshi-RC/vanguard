package middleware

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"

	"github.com/Mrigakshi-RC/vanguard/internal/handler"
)

type AllowLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter int, err error)
}

func RateLimitMiddleware(limiter AllowLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			redisKey := "rate_limit:events:" + ip
			allowed, retryAfter, err := limiter.Allow(r.Context(), redisKey)
			if err != nil {
				log.Println("Error checking rate limit", err)
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				handler.WriteJSONError(w, http.StatusTooManyRequests, "Too many requests, retry after "+strconv.Itoa(retryAfter))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
