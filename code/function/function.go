// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hello

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/run/v1"
)

// PubSubMessage is the payload of a Pub/Sub event.
// See the documentation for more details:
// https://cloud.google.com/pubsub/docs/reference/rest/v1/PubsubMessage
type PubSubMessage struct {
	Data []byte `json:"data"`
}

// BillingNotice is a translation of the data that gets sent via PUB/SUB when
// there is a billing overage
type BillingNotice struct {
	Name              string    `json:"budgetDisplayName"`
	ThresholdExceeded float32   `json:"alertThresholdExceeded"`
	Cost              float32   `json:"costAmount"`
	CostStart         time.Time `json:"costIntervalStart"`
	Budget            float32   `json:"budgetAmount"`
	BudgetType        string    `json:"budgetAmountType"`
	Code              string    `json:"currencyCode"`
}

// LimitUsage will read in Billing overages and shut down machine's accordingly.
func LimitUsage(ctx context.Context, m PubSubMessage) error {
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	label := os.Getenv("LABEL")

	over, err := budgetCheckbudgetExceeded(m)
	if err != nil {
		return fmt.Errorf("cannot properly check the budget: %s", err)
	}

	if !over {
		fmt.Println("Underbudget, no action needed.")
		return nil
	}
	fmt.Println("Overbudget, stopping VMs.")

	if err := manageRun(ctx, project, label); err != nil {
		return fmt.Errorf("cannot manage Cloud Run Services: %s", err)
	}

	fmt.Printf("Cost Sentry managed Cloud Run Services\n")

	if err := manageCompute(ctx, project, label); err != nil {
		return fmt.Errorf("cannot manage Cloud Run Services: %s", err)
	}

	fmt.Printf("Cost Sentry managed Compute Engine instances\n")
	return nil
}

func budgetCheckbudgetExceeded(m PubSubMessage) (bool, error) {
	data := string(m.Data)

	notice := BillingNotice{}
	if err := json.Unmarshal([]byte(data), &notice); err != nil {
		return false, fmt.Errorf("cannot unmarshall Pub/Sub message: %s", err)
	}

	if notice.Cost <= notice.Budget {
		return false, nil
	}

	return true, nil
}

func manageCompute(ctx context.Context, project, label string) error {
	filters := []string{
		"status = RUNNING",
		fmt.Sprintf("labels.%s = true", label),
	}

	svc, err := compute.NewService(ctx)
	if err != nil {
		return fmt.Errorf("cannont instaitate Compute Engine API service:: %s", err)
	}

	gceInstances, err := computeInstances(project, svc, filters)
	if err != nil {
		return fmt.Errorf("cannot get a list of Compute Engine Instances: %s", err)
	}

	if err := computeStop(project, svc, gceInstances); err != nil {
		return fmt.Errorf("cannot stop Compute Engine Instances: %s", err)
	}
	return nil
}

func manageRun(ctx context.Context, project, label string) error {
	svc, err := run.NewService(ctx)
	if err != nil {
		return fmt.Errorf("cannot instaitate Cloud Run API service: %s", err)
	}

	runServices, err := runServices(project, svc, label)
	if err != nil {
		return fmt.Errorf("cannot get a list of Cloud Run Services: %s", err)
	}

	if err := runDisable(project, svc, runServices); err != nil {
		return fmt.Errorf("cannot disable Cloud Run Services: %s", err)
	}
	fmt.Printf("Cost Sentry disabled %d Cloud Run Services\n", len(runServices))

	return nil
}

func find(sl []string, sub string) bool {
	for _, v := range sl {
		if v == sub {
			return true
		}
	}

	return false
}

func runDisable(project string, svc *run.APIService, serviceList []*run.Service) error {
	for _, s := range serviceList {

		location, ok := s.Metadata.Labels["cloud.googleapis.com/location"]
		if !ok {
			return fmt.Errorf("location incorrectly placed in Cloud Run metadata")
		}

		name := fmt.Sprintf("projects/%s/locations/%s/services/%s", project, location, s.Metadata.Name)

		iamPolicy, err := svc.Projects.Locations.Services.GetIamPolicy(name).Do()
		if err != nil {
			return fmt.Errorf("error getting IAM policy: %s", err)
		}

		for i, b := range iamPolicy.Bindings {
			if find(b.Members, "allUsers") {
				iamPolicy.Bindings[i] = nil
			}
		}

		setReq := &run.SetIamPolicyRequest{}
		setReq.Policy = iamPolicy

		if _, err := svc.Projects.Locations.Services.SetIamPolicy(name, setReq).Do(); err != nil {
			return fmt.Errorf("error disabling external access to services: %s", err)
		}

	}
	return nil
}

func runServices(project string, srv *run.APIService, label string) ([]*run.Service, error) {
	parent := fmt.Sprintf("projects/%s", project)
	l := fmt.Sprintf("%s=true", label)
	services := []*run.Service{}

	locations, err := srv.Projects.Locations.List(parent).Do()
	if err != nil {
		return services, fmt.Errorf("error getting Cloud Run locations: %s", err)
	}

	var wg sync.WaitGroup
	wg.Add(len(locations.Locations))

	for _, location := range locations.Locations {
		go func(loc *run.Location) error {
			defer wg.Done()

			lp := fmt.Sprintf("projects/%s/locations/%s", project, loc.LocationId)

			svcs, err := srv.Projects.Locations.Services.List(lp).LabelSelector(l).Do()
			if err != nil {
				return fmt.Errorf("error cannot get Cloud Run service for %s: %s", loc.LocationId, err)
			}

			services = append(services, svcs.Items...)

			return nil
		}(location)
	}

	wg.Wait()

	return services, nil
}

func computeInstances(project string, srv *compute.Service, filters []string) (*compute.InstanceList, error) {
	instances := &compute.InstanceList{}
	zoneListCall := srv.Zones.List(project)
	zoneList, err := zoneListCall.Do()
	if err != nil {
		return nil, fmt.Errorf("error - cannot get Compute Engine zone list: %s", err)
	}

	var wg sync.WaitGroup
	wg.Add(len(zoneList.Items))

	for _, zone := range zoneList.Items {
		go func(zone *compute.Zone) error {
			defer wg.Done()
			instanceListCall := srv.Instances.List(project, zone.Name)
			instanceListCall.Filter(strings.Join(filters[:], " "))
			instanceList, err := instanceListCall.Do()
			if err != nil {
				return fmt.Errorf("cannot get Compute Engine instance list: %s", err)
			}
			instances.Items = append(instances.Items, instanceList.Items...)
			return nil
		}(zone)
	}
	wg.Wait()
	return instances, nil
}

func computeStop(project string, srv *compute.Service, instanceList *compute.InstanceList) error {
	for _, v := range instanceList.Items {
		zoneStr := strings.Split(v.Zone, "/")
		zone := zoneStr[len(zoneStr)-1]
		stopCall := srv.Instances.Stop(project, zone, v.Name)

		if _, err := stopCall.Do(); err != nil {
			return fmt.Errorf("error stopping Compute Engine instances: %s", err)
		}
	}
	return nil
}
