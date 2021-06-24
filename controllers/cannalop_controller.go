/*
Copyright 2021.

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

package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1v1 "cannelop/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// CannalopReconciler reconciles a Cannalop object
type CannalopReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}
const (
	// deployment中的APP标签名
	APP_NAME = "cannal-op"
	// tomcat容器的端口号
	CONTAINER_PORT = 8080
	// 单个POD的CPU资源申请
	CPU_REQUEST = "100m"
	// 单个POD的CPU资源上限
	CPU_LIMIT = "100m"
	// 单个POD的内存资源申请
	MEM_REQUEST = "512Mi"
	// 单个POD的内存资源上限
	MEM_LIMIT = "512Mi"
)
//+kubebuilder:rbac:groups=v1.com.cannal,resources=cannalops,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=v1.com.cannal,resources=cannalops/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=v1.com.cannal,resources=cannalops/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cannalop object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *CannalopReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log:= r.Log.WithValues("cannalop", req.NamespacedName)
	log.Info("ok, start reconcile logic")

	//检查是否注册
	canalop := &v1v1.Cannalop{}
	err := r.Get(ctx, req.NamespacedName, canalop)
	if err != nil {
		// 如果没有实例，就返回空结果，这样外部就不再立即调用Reconcile方法了
		if errors.IsNotFound(err) {
			log.Info("2.1. instance not found, maybe removed")
			return reconcile.Result{}, nil
		}

		log.Error(err, "2.2 error")
		// 返回错误信息给外部
		return ctrl.Result{}, err
	}

	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, req.NamespacedName, deployment)
	// 查找时发生异常，以及查出来没有结果的处理逻辑
	if err != nil {
		// 如果没有实例就要创建了
		if errors.IsNotFound(err) {
			log.Info("4. deployment not exists")

			// 为空的时候不需要创建deploy 同步
			if *canalop.Spec.Replications == 0{
				log.Info("5.1 not need deployment")
				// 返回
				return ctrl.Result{}, nil
			}
			// 先要创建service
			if err = createServiceIfNotExists(ctx, r, canalop, req); err != nil {
				log.Error(err, "5.2 error")
				// 返回错误信息给外部
				return ctrl.Result{}, err
			}

			// 立即创建deployment
			if err = createDeployment(ctx, r, canalop); err != nil {
				log.Error(err, "5.3 error")
				// 返回错误信息给外部
				return ctrl.Result{}, err
			}



			// 创建成功就可以返回了
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "7. error")
			// 返回错误信息给外部
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CannalopReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1v1.Cannalop{}).
		Complete(r)
}

// 新建service
func createServiceIfNotExists(ctx context.Context, r *CannalopReconciler, cannal *v1v1.Cannalop, req ctrl.Request) error {
	log := r.Log.WithValues("func", "createService")

	service := &corev1.Service{}

	err := r.Get(ctx, req.NamespacedName, service)

	// 如果查询结果没有错误，证明service正常，就不做任何操作
	if err == nil {
		log.Info("service exists")
		return nil
	}

	// 如果错误不是NotFound，就返回错误
	if !errors.IsNotFound(err) {
		log.Error(err, "query service error")
		return err
	}
	// 实例化一个数据结构
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cannal.Namespace,
			Name:      cannal.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:     "canal",
				Port:     11111,
				NodePort: *cannal.Spec.ServicePort,
			},
			},
			Selector: map[string]string{
				"app": APP_NAME,
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}

	// 这一步非常关键！
	// 建立关联后，删除elasticweb资源时就会将deployment也删除掉
	log.Info("set reference")
	if err := controllerutil.SetControllerReference(cannal, service, r.Scheme); err != nil {
		log.Error(err, "SetControllerReference error")
		return err
	}

	// 创建service
	log.Info("start create service")
	if err := r.Create(ctx, service); err != nil {
		log.Error(err, "create service error")
		return err
	}

	log.Info("create service success")

	return nil
}
func createDeployment(ctx context.Context, r *CannalopReconciler, cannal *v1v1.Cannalop) error {
	log := r.Log.WithValues("func", "createDeployment")

	// 实例化一个数据结构
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cannal.Namespace,
			Name:      cannal.Name,
		},
		Spec: appsv1.DeploymentSpec{
			// 副本数是计算出来的
			Replicas: pointer.Int32Ptr(*cannal.Spec.Replications),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": APP_NAME,
				},
			},

			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": APP_NAME,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: APP_NAME,
							// 用指定的镜像
							Image:           cannal.Spec.Image,
							ImagePullPolicy: "IfNotPresent",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: CONTAINER_PORT,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse(CPU_REQUEST),
									"memory": resource.MustParse(MEM_REQUEST),
								},
								Limits: corev1.ResourceList{
									"cpu":    resource.MustParse(CPU_LIMIT),
									"memory": resource.MustParse(MEM_LIMIT),
								},
							},
						},
					},
				},
			},
		},
	}

	// 这一步非常关键！
	// 建立关联后，删除elasticweb资源时就会将deployment也删除掉
	log.Info("set reference")
	if err := controllerutil.SetControllerReference(cannal, deployment, r.Scheme); err != nil {
		log.Error(err, "SetControllerReference error")
		return err
	}

	// 创建deployment
	log.Info("start create deployment")
	if err := r.Create(ctx, deployment); err != nil {
		log.Error(err, "create deployment error")
		return err
	}

	log.Info("create deployment success")

	return nil
}
// 增加pod之后，更新crd里面的副本数

