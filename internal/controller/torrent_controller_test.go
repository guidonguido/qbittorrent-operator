package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

var _ = Describe("Torrent Controller", func() {
	Context("When no TCC exists in the namespace", func() {
		const resourceName = "test-torrent-no-tcc"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the Torrent resource without any TCC in the namespace")
			torrent := &torrentv1alpha1.Torrent{}
			err := k8sClient.Get(ctx, typeNamespacedName, torrent)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.Torrent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentSpec{
						MagnetURI: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.Torrent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				// Remove finalizer to allow deletion
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should add a finalizer on first reconcile", func() {
			controllerReconciler := &TorrentReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				ClientPool: qbittorrent.NewClientPool(5 * time.Minute),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			torrent := &torrentv1alpha1.Torrent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, torrent)).To(Succeed())
			Expect(torrent.Finalizers).To(ContainElement(TorrentFinalizer))
		})

		It("should set Degraded condition when no TCC is found (auto-discovery)", func() {
			controllerReconciler := &TorrentReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				ClientPool: qbittorrent.NewClientPool(5 * time.Minute),
			}

			// First reconcile: adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: attempts to resolve TCC
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			torrent := &torrentv1alpha1.Torrent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, torrent)).To(Succeed())
			Expect(torrent.Status.Conditions).To(HaveLen(1))
			Expect(torrent.Status.Conditions[0].Type).To(Equal(TypeDegradedTorrent))
			Expect(torrent.Status.Conditions[0].Reason).To(Equal("ClientResolutionFailed"))
		})
	})

	Context("When an explicit clientConfigRef references a non-existent TCC", func() {
		const resourceName = "test-torrent-bad-ref"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the Torrent resource with a non-existent clientConfigRef")
			torrent := &torrentv1alpha1.Torrent{}
			err := k8sClient.Get(ctx, typeNamespacedName, torrent)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.Torrent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentSpec{
						MagnetURI: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny",
						ClientConfigRef: &torrentv1alpha1.LocalObjectReference{
							Name: "nonexistent-tcc",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.Torrent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Degraded condition when referenced TCC is not found", func() {
			controllerReconciler := &TorrentReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				ClientPool: qbittorrent.NewClientPool(5 * time.Minute),
			}

			// First reconcile: adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: attempts to resolve TCC
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			torrent := &torrentv1alpha1.Torrent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, torrent)).To(Succeed())
			Expect(torrent.Status.Conditions).To(HaveLen(1))
			Expect(torrent.Status.Conditions[0].Type).To(Equal(TypeDegradedTorrent))
			Expect(torrent.Status.Conditions[0].Reason).To(Equal("ClientResolutionFailed"))
		})
	})

	Context("When a TCC exists but is not Available", func() {
		const resourceName = "test-torrent-tcc-not-ready"
		const tccName = "test-tcc-not-ready"
		const secretName = "test-tcc-not-ready-creds"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the credentials secret")
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret)
			if errors.IsNotFound(err) {
				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte("admin"),
						"password": []byte("password"),
					},
				}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			By("creating a TCC without Available condition")
			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: tccName, Namespace: "default"}, tcc)
			if errors.IsNotFound(err) {
				tcc = &torrentv1alpha1.TorrentClientConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tccName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentClientConfigurationSpec{
						URL: "http://qbittorrent:8080",
						CredentialsSecret: torrentv1alpha1.SecretReference{
							Name: secretName,
						},
					},
				}
				Expect(k8sClient.Create(ctx, tcc)).To(Succeed())
			}

			By("creating the Torrent resource referencing the TCC")
			torrent := &torrentv1alpha1.Torrent{}
			err = k8sClient.Get(ctx, typeNamespacedName, torrent)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.Torrent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentSpec{
						MagnetURI: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny",
						ClientConfigRef: &torrentv1alpha1.LocalObjectReference{
							Name: tccName,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.Torrent{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tccName, Namespace: "default"}, tcc); err == nil {
				Expect(k8sClient.Delete(ctx, tcc)).To(Succeed())
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secret); err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should set Degraded condition when TCC is not Available", func() {
			controllerReconciler := &TorrentReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				ClientPool: qbittorrent.NewClientPool(5 * time.Minute),
			}

			// First reconcile: adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: attempts to use TCC (which has no Available condition)
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			torrent := &torrentv1alpha1.Torrent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, torrent)).To(Succeed())
			Expect(torrent.Status.Conditions).To(HaveLen(1))
			Expect(torrent.Status.Conditions[0].Type).To(Equal(TypeDegradedTorrent))
			Expect(torrent.Status.Conditions[0].Reason).To(Equal("ClientResolutionFailed"))
		})
	})

	Context("When multiple TCCs exist and no explicit ref is set", func() {
		const resourceName = "test-torrent-multi-tcc"
		const tcc1Name = "test-tcc-multi-1"
		const tcc2Name = "test-tcc-multi-2"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating two TCCs")
			for _, name := range []string{tcc1Name, tcc2Name} {
				tcc := &torrentv1alpha1.TorrentClientConfiguration{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, tcc)
				if errors.IsNotFound(err) {
					tcc = &torrentv1alpha1.TorrentClientConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: "default",
						},
						Spec: torrentv1alpha1.TorrentClientConfigurationSpec{
							URL: "http://qbittorrent:8080",
							CredentialsSecret: torrentv1alpha1.SecretReference{
								Name: "some-secret",
							},
						},
					}
					Expect(k8sClient.Create(ctx, tcc)).To(Succeed())
				}
			}

			By("creating the Torrent resource without clientConfigRef")
			torrent := &torrentv1alpha1.Torrent{}
			err := k8sClient.Get(ctx, typeNamespacedName, torrent)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.Torrent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentSpec{
						MagnetURI: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.Torrent{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			for _, name := range []string{tcc1Name, tcc2Name} {
				tcc := &torrentv1alpha1.TorrentClientConfiguration{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, tcc); err == nil {
					Expect(k8sClient.Delete(ctx, tcc)).To(Succeed())
				}
			}
		})

		It("should set Degraded condition when multiple TCCs exist", func() {
			controllerReconciler := &TorrentReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				ClientPool: qbittorrent.NewClientPool(5 * time.Minute),
			}

			// First reconcile: adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: should fail with ambiguous TCC
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			torrent := &torrentv1alpha1.Torrent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, torrent)).To(Succeed())
			Expect(torrent.Status.Conditions).To(HaveLen(1))
			Expect(torrent.Status.Conditions[0].Type).To(Equal(TypeDegradedTorrent))
			Expect(torrent.Status.Conditions[0].Reason).To(Equal("ClientResolutionFailed"))
			Expect(torrent.Status.Conditions[0].Message).To(ContainSubstring("multiple"))
		})
	})
})
