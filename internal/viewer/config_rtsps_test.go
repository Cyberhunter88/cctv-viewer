package viewer

import "testing"

func TestRTSPSCertificateAndKeyBelongTogether(t *testing.T) {
	t.Setenv("CTV_LIVE_RTSPS_CERT", "/certs/share.crt")
	t.Setenv("CTV_LIVE_RTSPS_KEY", "")
	if _, e := LoadConfig(); e == nil {
		t.Fatal("a certificate without key was accepted")
	}
	t.Setenv("CTV_LIVE_RTSPS_KEY", "/certs/share.key")
	c, e := LoadConfig()
	if e != nil || c.LiveRTSPSCert != "/certs/share.crt" || c.LiveRTSPSKey != "/certs/share.key" {
		t.Fatal(c.LiveRTSPSCert, c.LiveRTSPSKey, e)
	}
	if !validShareServerURL("rtsps://viewer.example.org:8554") || validShareServerURL("https://viewer.example.org:8554") {
		t.Fatal("share URL must allow rtsp and rtsps only")
	}
}
