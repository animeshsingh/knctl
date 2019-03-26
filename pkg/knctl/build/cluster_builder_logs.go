/*
Copyright 2018 The Knative Authors

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

package build

import (
	"fmt"

	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/cppforlife/knctl/pkg/knctl/logs"
	corev1 "k8s.io/api/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type ClusterBuilderLogs struct {
	waiter           BuildWaiter
	podsGetterClient typedcorev1.PodsGetter
}

func NewClusterBuilderLogs(
	waiter BuildWaiter,
	podsGetterClient typedcorev1.PodsGetter,
) ClusterBuilderLogs {
	return ClusterBuilderLogs{waiter, podsGetterClient}
}

func (l ClusterBuilderLogs) Tail(ui ui.UI, cancelCh chan struct{}) error { // TODO cancel
	build, pod, err := l.waiter.WaitForClusterBuilderPodAssignment(cancelCh)
	if err != nil {
		return fmt.Errorf("Waiting for build to be assigned a pod: %s", err)
	}

	if build.Status.Cluster == nil {
		return fmt.Errorf("Expected build to have cluster configuration assigned")
	}

	podsClient := l.podsGetterClient.Pods(build.Status.Cluster.Namespace)

	statusWatcher := PodTerminalStatusWatcher{*pod, podsClient}
	cancelPodTailCh := make(chan struct{})

	done, _, _ := statusWatcher.IsDone()
	if !done {
		// Wait for pod to reach one of its terminal states
		// to make sure we've collected all of the logs;
		// log stream does not end on its own when pod phase is 'Failed'.
		go func() {
			_, err := statusWatcher.Wait(cancelPodTailCh)
			if err != nil {
				ui.BeginLinef("Pod status waiting error: %s\n", err)
			}

			// TODO logs may get truncated since we are terminating
			// tailing without waiting for all logs to drain
			close(cancelPodTailCh)
		}()
	}

	tagFunc := func(cont corev1.Container) string { return cont.Name }

	err = logs.NewPodLog(*pod, podsClient, tagFunc, logs.PodLogOpts{Follow: !done}).TailAll(ui, cancelPodTailCh)
	if err != nil {
		ui.BeginLinef("Pod logs tailing error: %s\n", err)
	}

	return nil
}
