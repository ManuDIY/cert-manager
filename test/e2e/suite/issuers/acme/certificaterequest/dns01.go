/*
Copyright 2019 The Jetstack cert-manager contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certificate

import (
	"crypto/x509"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/jetstack/cert-manager/test/e2e/framework"
	"github.com/jetstack/cert-manager/test/e2e/framework/addon"
	"github.com/jetstack/cert-manager/test/e2e/suite/issuers/acme/dnsproviders"
	"github.com/jetstack/cert-manager/test/e2e/util"
	"github.com/jetstack/cert-manager/test/util/generate"
)

type dns01Provider interface {
	Details() *dnsproviders.Details
	SetNamespace(string)

	addon.Addon
}

const testingACMEEmail = "e2e@cert-manager.io"
const testingACMEPrivateKey = "test-acme-private-key"

var _ = framework.CertManagerDescribe("ACME CertificateRequest (DNS01)", func() {
	// TODO: add additional DNS provider configs here
	cf := &dnsproviders.Cloudflare{}

	testDNSProvider("cloudflare", cf)
})

func testDNSProvider(name string, p dns01Provider) bool {
	return Context("With "+name+" credentials configured", func() {
		f := framework.NewDefaultFramework("create-acme-certificate-request-dns01-" + name)
		h := f.Helper()

		BeforeEach(func() {
			p.SetNamespace(f.Namespace.Name)
		})

		f.RequireAddon(p)

		issuerName := "test-acme-issuer"
		certificateRequestName := "test-acme-certificate-request"
		dnsDomain := ""

		BeforeEach(func() {
			dnsDomain = p.Details().NewTestDomain()

			By("Creating an Issuer")
			issuer := generate.Issuer(generate.IssuerConfig{
				Name:              issuerName,
				Namespace:         f.Namespace.Name,
				ACMESkipTLSVerify: true,
				// Hardcode this to the acme staging endpoint now due to issues with pebble dns resolution
				ACMEServer: "https://acme-staging-v02.api.letsencrypt.org/directory",
				// ACMEServer:         framework.TestContext.ACMEURL,
				ACMEEmail:          testingACMEEmail,
				ACMEPrivateKeyName: testingACMEPrivateKey,
				Solvers: []v1alpha1.ACMEChallengeSolver{
					{
						DNS01: &p.Details().ProviderConfig,
					},
				},
			})
			issuer, err := f.CertManagerClientSet.CertmanagerV1alpha1().Issuers(f.Namespace.Name).Create(issuer)
			Expect(err).NotTo(HaveOccurred())
			By("Waiting for Issuer to become Ready")
			err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha1().Issuers(f.Namespace.Name),
				issuerName,
				v1alpha1.IssuerCondition{
					Type:   v1alpha1.IssuerConditionReady,
					Status: v1alpha1.ConditionTrue,
				})
			Expect(err).NotTo(HaveOccurred())
			By("Verifying the ACME account URI is set")
			err = util.WaitForIssuerStatusFunc(f.CertManagerClientSet.CertmanagerV1alpha1().Issuers(f.Namespace.Name),
				issuerName,
				func(i *v1alpha1.Issuer) (bool, error) {
					if i.GetStatus().ACMEStatus().URI == "" {
						return false, nil
					}
					return true, nil
				})
			Expect(err).NotTo(HaveOccurred())
			By("Verifying ACME account private key exists")
			secret, err := f.KubeClientSet.CoreV1().Secrets(f.Namespace.Name).Get(testingACMEPrivateKey, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			if len(secret.Data) != 1 {
				Fail("Expected 1 key in ACME account private key secret, but there was %d", len(secret.Data))
			}
		})

		AfterEach(func() {
			By("Cleaning up")
			f.CertManagerClientSet.CertmanagerV1alpha1().Issuers(f.Namespace.Name).Delete(issuerName, nil)
			f.KubeClientSet.CoreV1().Secrets(f.Namespace.Name).Delete(testingACMEPrivateKey, nil)
		})

		It("should obtain a signed certificate for a regular domain", func() {
			By("Creating a CertificateRequest")

			crClient := f.CertManagerClientSet.CertmanagerV1alpha1().CertificateRequests(f.Namespace.Name)

			cr, key, err := util.NewCertManagerBasicCertificateRequest(certificateRequestName, issuerName, v1alpha1.IssuerKind, nil,
				[]string{dnsDomain}, nil, nil, x509.RSA)
			Expect(err).NotTo(HaveOccurred())

			cr, err = crClient.Create(cr)
			Expect(err).NotTo(HaveOccurred())
			err = h.WaitCertificateRequestIssuedValid(f.Namespace.Name, certificateRequestName, time.Minute*5, key)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a signed certificate for a wildcard domain", func() {
			By("Creating a CertificateRequest")

			cr, key, err := util.NewCertManagerBasicCertificateRequest(certificateRequestName, issuerName, v1alpha1.IssuerKind, nil,
				[]string{"*." + dnsDomain}, nil, nil, x509.RSA)
			Expect(err).NotTo(HaveOccurred())

			cr, err = f.CertManagerClientSet.CertmanagerV1alpha1().CertificateRequests(f.Namespace.Name).Create(cr)
			Expect(err).NotTo(HaveOccurred())
			err = h.WaitCertificateRequestIssuedValid(f.Namespace.Name, certificateRequestName, time.Minute*5, key)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a signed certificate for a wildcard and apex domain", func() {
			By("Creating a CertificateRequest")

			cr, key, err := util.NewCertManagerBasicCertificateRequest(certificateRequestName, issuerName, v1alpha1.IssuerKind, nil,
				[]string{"*." + dnsDomain, dnsDomain}, nil, nil, x509.RSA)
			Expect(err).NotTo(HaveOccurred())

			cr, err = f.CertManagerClientSet.CertmanagerV1alpha1().CertificateRequests(f.Namespace.Name).Create(cr)
			Expect(err).NotTo(HaveOccurred())
			// use a longer timeout for this, as it requires performing 2 dns validations in serial
			err = h.WaitCertificateRequestIssuedValid(f.Namespace.Name, certificateRequestName, time.Minute*10, key)
			Expect(err).NotTo(HaveOccurred())
		})
	})
}
