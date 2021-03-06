// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package gcpbotanist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	gardenv1beta1 "github.com/gardener/gardener/pkg/apis/garden/v1beta1"
	"github.com/gardener/gardener/pkg/operation"
	"github.com/gardener/gardener/pkg/operation/common"

	"github.com/gardener/gardener-extensions/controllers/provider-gcp/pkg/apis/gcp"
	gcpv1alpha1 "github.com/gardener/gardener-extensions/controllers/provider-gcp/pkg/apis/gcp/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// IMPORTANT NOTICE
// The following part is only temporarily needed until we have completed the Extensibility epic
// and moved out all provider specifics.
// IMPORTANT NOTICE

var (
	scheme  *runtime.Scheme
	decoder runtime.Decoder
)

func init() {
	scheme = runtime.NewScheme()

	// Workaround for incompatible kubernetes dependencies in gardener/gardener and
	// gardener/gardener-extensions.
	gcpSchemeBuilder := runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(gcp.SchemeGroupVersion, &gcp.InfrastructureConfig{}, &gcp.InfrastructureStatus{})
		return nil
	})
	gcpv1alpha1SchemeBuilder := runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(gcpv1alpha1.SchemeGroupVersion, &gcpv1alpha1.InfrastructureConfig{}, &gcpv1alpha1.InfrastructureStatus{})
		return nil
	})
	schemeBuilder := runtime.NewSchemeBuilder(
		gcpv1alpha1SchemeBuilder.AddToScheme,
		gcpSchemeBuilder.AddToScheme,
	)
	utilruntime.Must(schemeBuilder.AddToScheme(scheme))

	decoder = serializer.NewCodecFactory(scheme).UniversalDecoder()
}

func infrastructureStatusFromInfrastructure(raw []byte) (*gcpv1alpha1.InfrastructureStatus, error) {
	config := &gcpv1alpha1.InfrastructureStatus{}
	if _, _, err := decoder.Decode(raw, nil, config); err != nil {
		return nil, err
	}
	return config, nil
}

func findSubnetByPurpose(subnets []gcpv1alpha1.Subnet, purpose gcpv1alpha1.SubnetPurpose) (*gcpv1alpha1.Subnet, error) {
	for _, subnet := range subnets {
		if subnet.Purpose == purpose {
			return &subnet, nil
		}
	}
	return nil, fmt.Errorf("cannot find subnet with purpose %q", purpose)
}

// IMPORTANT NOTICE
// The above part is only temporarily needed until we have completed the Extensibility epic
// and moved out all provider specifics.
// IMPORTANT NOTICE

// New takes an operation object <o> and creates a new GCPBotanist object.
func New(o *operation.Operation, purpose string) (*GCPBotanist, error) {
	var cloudProvider gardenv1beta1.CloudProvider

	switch purpose {
	case common.CloudPurposeShoot:
		cloudProvider = o.Shoot.CloudProvider
	case common.CloudPurposeSeed:
		cloudProvider = o.Seed.CloudProvider
	}

	if cloudProvider != gardenv1beta1.CloudProviderGCP {
		return nil, errors.New("cannot instantiate an GCP botanist if neither Shoot nor Seed cluster specifies GCP")
	}

	// Read vpc name out of the Shoot manifest
	vpcName := ""
	if purpose == common.CloudPurposeShoot {
		if gcp := o.Shoot.Info.Spec.Cloud.GCP; gcp != nil {
			if vpc := gcp.Networks.VPC; vpc != nil {
				vpcName = vpc.Name
			}
		}
	}

	// Read project id out of the service account
	var serviceAccountJSON []byte
	switch purpose {
	case common.CloudPurposeShoot:
		serviceAccountJSON = o.Shoot.Secret.Data[ServiceAccountJSON]
	case common.CloudPurposeSeed:
		serviceAccountJSON = o.Seed.Secret.Data[ServiceAccountJSON]
	}

	project, err := ExtractProjectID(serviceAccountJSON)
	if err != nil {
		return nil, err
	}

	// Minify serviceaccount json to allow injection into Terraform environment
	minifiedServiceAccount, err := MinifyServiceAccount(serviceAccountJSON)
	if err != nil {
		return nil, err
	}

	return &GCPBotanist{
		Operation:              o,
		CloudProviderName:      "gce",
		VPCName:                vpcName,
		Project:                project,
		MinifiedServiceAccount: minifiedServiceAccount,
	}, nil
}

// GetCloudProviderName returns the Kubernetes cloud provider name for this cloud.
func (b *GCPBotanist) GetCloudProviderName() string {
	return b.CloudProviderName
}

// MinifyServiceAccount uses the provided service account JSON objects and minifies it.
// This is required when you want to inject it as environment variable into Terraform.
func MinifyServiceAccount(serviceAccountJSON []byte) (string, error) {
	buf := new(bytes.Buffer)
	if err := json.Compact(buf, serviceAccountJSON); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ExtractProjectID extracts the value of the key "project_id" from the service account
// JSON document.
func ExtractProjectID(ServiceAccountJSON []byte) (string, error) {
	var j struct {
		Project string `json:"project_id"`
	}
	if err := json.Unmarshal(ServiceAccountJSON, &j); err != nil {
		return "Error", err
	}
	return j.Project, nil
}
