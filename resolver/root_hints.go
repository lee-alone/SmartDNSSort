package resolver

import (
	"os"
	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// LoadRootHints 从 named.cache 文件加载根服务器地址
func LoadRootHints(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var roots []string
	zp := dns.NewZoneParser(file, ".", filePath)

	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		switch r := rr.(type) {
		case *dns.A:
			roots = append(roots, r.A.String())
		case *dns.AAAA:
			roots = append(roots, r.AAAA.String())
		}
	}

	if err := zp.Err(); err != nil {
		return nil, err
	}

	if len(roots) == 0 {
		logger.Warnf("No root hints found in %s", filePath)
	} else {
		logger.Infof("Loaded %d root hints from %s", len(roots), filePath)
	}

	return roots, nil
}
