package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
)

var _ = Describe("TorrentClientConfiguration Controller", func() {
	Context("When the referenced secret does not exist", func() {
		const resourceName = "test-tcc-no-secret"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the TCC resource referencing a non-existent secret")
			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			err := k8sClient.Get(ctx, typeNamespacedName, tcc)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.TorrentClientConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentClientConfigurationSpec{
						URL: "http://qbittorrent:8080",
						CredentialsSecret: torrentv1alpha1.SecretReference{
							Name: "nonexistent-secret",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.TorrentClientConfiguration{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Degraded condition when secret is not found", func() {
			controllerReconciler := &TorrentClientConfigurationReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tcc)).To(Succeed())
			Expect(tcc.Status.Connected).To(BeFalse())
			Expect(tcc.Status.Conditions).To(HaveLen(1))
			Expect(tcc.Status.Conditions[0].Type).To(Equal(TypeDegradedTCC))
			Expect(tcc.Status.Conditions[0].Reason).To(Equal("SecretNotFound"))
		})
	})

	Context("When the referenced secret has invalid keys", func() {
		const resourceName = "test-tcc-invalid-secret"
		const secretName = "test-tcc-invalid-creds"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating a secret without the required keys")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"wrong-key": []byte("value"),
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, &corev1.Secret{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			By("creating the TCC resource")
			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			err = k8sClient.Get(ctx, typeNamespacedName, tcc)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.TorrentClientConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentClientConfigurationSpec{
						URL: "http://qbittorrent:8080",
						CredentialsSecret: torrentv1alpha1.SecretReference{
							Name: secretName,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.TorrentClientConfiguration{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret); err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should set Degraded condition when secret keys are invalid", func() {
			controllerReconciler := &TorrentClientConfigurationReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tcc)).To(Succeed())
			Expect(tcc.Status.Connected).To(BeFalse())
			Expect(tcc.Status.Conditions).To(HaveLen(1))
			Expect(tcc.Status.Conditions[0].Reason).To(Equal("SecretInvalid"))
		})
	})
})
