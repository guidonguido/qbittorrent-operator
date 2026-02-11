package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
)

var _ = Describe("TorrentServer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-torrentserver"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TorrentServer")
			ts := &torrentv1alpha1.TorrentServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, ts)
			if err != nil && errors.IsNotFound(err) {
				resource := &torrentv1alpha1.TorrentServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: torrentv1alpha1.TorrentServerSpec{
						Image: "lscr.io/linuxserver/qbittorrent:amd64-5.1.4",
						Env: []corev1.EnvVar{
							{Name: "PUID", Value: "0"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &torrentv1alpha1.TorrentServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Removing finalizers and cleaning up TorrentServer")
				// Remove finalizers first to allow deletion
				if len(resource.Finalizers) > 0 {
					resource.Finalizers = nil
					Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				}
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create owned resources after reconciliation", func() {
			controllerReconciler := &TorrentServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile multiple times: 1st adds finalizer, 2nd+ creates resources
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify Deployment was created
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("lscr.io/linuxserver/qbittorrent:amd64-5.1.4"))

			// Verify no init containers when OperatorImage is not set
			Expect(deployment.Spec.Template.Spec.InitContainers).To(BeEmpty())

			// Verify Service was created
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, svc)).To(Succeed())

			// Verify config PVC was created
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-config", Namespace: "default",
			}, pvc)).To(Succeed())

			// Verify credentials Secret was auto-generated
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-credentials", Namespace: "default",
			}, secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("username"))
			Expect(secret.Data).To(HaveKey("password"))

			// Verify TCC was created
			tcc := &torrentv1alpha1.TorrentClientConfiguration{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-client-config", Namespace: "default",
			}, tcc)).To(Succeed())
			Expect(tcc.Spec.URL).To(ContainSubstring(resourceName))

			// Verify status was updated
			ts := &torrentv1alpha1.TorrentServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ts)).To(Succeed())
			Expect(ts.Status.DeploymentName).To(Equal(resourceName))
			Expect(ts.Status.ServiceName).To(Equal(resourceName))
			Expect(ts.Status.ConfigPVCName).To(Equal(resourceName + "-config"))
		})

		It("should include init container when OperatorImage is set", func() {
			controllerReconciler := &TorrentServerReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				OperatorImage: "ghcr.io/guidonguido/qbittorrent-operator:test",
			}

			// Reconcile multiple times: 1st adds finalizer, 2nd+ creates resources
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify Deployment has init container
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, deployment)).To(Succeed())

			Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			initContainer := deployment.Spec.Template.Spec.InitContainers[0]
			Expect(initContainer.Name).To(Equal("config-init"))
			Expect(initContainer.Image).To(Equal("ghcr.io/guidonguido/qbittorrent-operator:test"))
			Expect(initContainer.Command).To(Equal([]string{"/manager", "config-init"}))

			// Verify credentials volume is present
			volumeNames := make([]string, len(deployment.Spec.Template.Spec.Volumes))
			for i, v := range deployment.Spec.Template.Spec.Volumes {
				volumeNames[i] = v.Name
			}
			Expect(volumeNames).To(ContainElement("credentials"))
		})
	})
})
