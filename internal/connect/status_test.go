package connect

import (
	"testing"
	"time"
)

func TestCertificateReadinessReportsTransitions(t *testing.T) {
	var states []CertificateStatus

	agent := Agent{CertificateStatus: func(status CertificateStatus) { states = append(states, status) }}
	report := agent.certificateReporter()
	expiry := time.Now().Add(time.Hour)

	report(CertificateStatus{})
	report(CertificateStatus{Ready: true, NotAfter: expiry})
	report(CertificateStatus{Ready: true, NotAfter: expiry})
	report(CertificateStatus{Ready: true, NotAfter: expiry.Add(time.Hour)})
	report(CertificateStatus{})

	if len(states) != 4 || !states[1].Ready || !states[2].NotAfter.After(states[1].NotAfter) || states[3].Ready {
		t.Fatal(states)
	}
}
