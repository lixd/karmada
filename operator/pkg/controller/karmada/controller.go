package karmada

import (
	"context"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorv1alpha1 "github.com/karmada-io/karmada/operator/pkg/apis/operator/v1alpha1"
)

const (
	// ControllerName is the controller name that will be used when reporting events.
	ControllerName = "karmada-operator-controller"

	// ControllerFinalizerName is the name of the karmada controller finalizer
	ControllerFinalizerName = "operator.karmada.io/finalizer"
)

// Controller controls the Karmada resource.
type Controller struct {
	client.Client
	Config        *rest.Config
	EventRecorder record.EventRecorder
}

// Reconcile performs a full reconciliation for the object referred to by the Request.
// The Controller will requeue the Request to be processed again if an error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (ctrl *Controller) Reconcile(ctx context.Context, req controllerruntime.Request) (controllerruntime.Result, error) {
	startTime := time.Now()
	klog.V(4).InfoS("Started syncing karmada", "karmada", req, "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing karmada", "karmada", req, "duration", time.Since(startTime))
	}()

	karmada := &operatorv1alpha1.Karmada{}
	if err := ctrl.Get(ctx, req.NamespacedName, karmada); err != nil {
		// The resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			klog.V(2).InfoS("Karmada has been deleted", "karmada", req)
			return controllerruntime.Result{}, nil
		}
		return controllerruntime.Result{}, err
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if karmada.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(karmada, ControllerFinalizerName) {
			controllerutil.AddFinalizer(karmada, ControllerFinalizerName)
			if err := ctrl.Update(ctx, karmada); err != nil {
				return controllerruntime.Result{}, err
			}
			// set to initializing phase
			deepCopy := karmada.DeepCopy()
			deepCopy.Status.Phase = operatorv1alpha1.KarmadaInitializing
			if !reflect.DeepEqual(karmada, deepCopy) {
				if err := ctrl.Status().Update(ctx, deepCopy); err != nil {
					return controllerruntime.Result{}, err
				}
			}
		}
	} else {
		// without our finalizer, skip reconciliation
		if !controllerutil.ContainsFinalizer(karmada, ControllerFinalizerName) {
			return controllerruntime.Result{}, nil
		}
		// set to terminating phase
		deepCopy := karmada.DeepCopy()
		deepCopy.Status.Phase = operatorv1alpha1.KarmadaTerminating
		if !reflect.DeepEqual(karmada, deepCopy) {
			if err := ctrl.Status().Update(ctx, deepCopy); err != nil {
				return controllerruntime.Result{}, err
			}
		}
		// The object is being deleted, deinitialize karmada
		planner, err := KarmadaDeInit(karmada, ctrl.Client, ctrl.Config)
		if err != nil {
			return controllerruntime.Result{}, err
		}
		if err := planner.Execute(); err != nil {
			return controllerruntime.Result{}, err
		}
		// remove finalizer
		return ctrl.removeFinalizer(ctx, karmada)
	}
	klog.V(2).InfoS("Reconciling karmada", "name", req.Name)

	if karmada.Status.Phase == operatorv1alpha1.KarmadaInitializing {
		// if not running,execute planner
		// The object is being deleted, deinitialize karmada
		planner, err := KarmadaInit(karmada, ctrl.Client, ctrl.Config)
		if err != nil {
			return controllerruntime.Result{}, err
		}
		if err := planner.Execute(); err != nil {
			return controllerruntime.Result{}, err
		}
		// set to running phase
		deepCopy := karmada.DeepCopy()
		deepCopy.Status.Phase = operatorv1alpha1.KarmadaRunning
		if !reflect.DeepEqual(karmada, deepCopy) {
			if err := ctrl.Status().Update(ctx, deepCopy); err != nil {
				return controllerruntime.Result{}, err
			}
		}
	}

	return controllerruntime.Result{}, nil
}

func (ctrl *Controller) removeFinalizer(ctx context.Context, karmada *operatorv1alpha1.Karmada) (controllerruntime.Result, error) {
	if controllerutil.ContainsFinalizer(karmada, ControllerFinalizerName) {
		controllerutil.RemoveFinalizer(karmada, ControllerFinalizerName)

		if err := ctrl.Update(ctx, karmada); err != nil {
			return controllerruntime.Result{}, err
		}
	}

	return controllerruntime.Result{}, nil
}

// SetupWithManager creates a controller and register to controller manager.
func (ctrl *Controller) SetupWithManager(mgr controllerruntime.Manager) error {
	return controllerruntime.NewControllerManagedBy(mgr).For(&operatorv1alpha1.Karmada{}).Complete(ctrl)
}
