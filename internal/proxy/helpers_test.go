package proxy_test

import (
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
)

func reqlogList() reqlog.ListParams {
	return reqlog.ListParams{Limit: 50}
}

func reqlogListSearch(q string) reqlog.ListParams {
	return reqlog.ListParams{Limit: 50, Search: q}
}
