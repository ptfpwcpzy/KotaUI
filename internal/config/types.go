package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const Version = "1.0.0"

// PanelLocation is the calendar timezone used for client expiry dates.
var PanelLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return location
}()

type Runtime struct {
	DataDir          string
	Listen           string
	PanelPath        string
	SubscriptionPort int
	Domain           string
	CertificateType  string
	TLSCert          string
	TLSKey           string
	AdminUser        string
	AdminPassword    string
	SingBoxBin       string
	SingBoxConfig    string
	ManageSingBox    bool
	StatsPort        int
}
