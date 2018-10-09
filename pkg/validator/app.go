// Copyright 2018 BlueData Software, Inc.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	kdv1 "github.com/bluek8s/kubedirector/pkg/apis/kubedirector.bluedata.io/v1alpha1"
	"github.com/bluek8s/kubedirector/pkg/catalog"
	"github.com/bluek8s/kubedirector/pkg/reconciler"
	"github.com/bluek8s/kubedirector/pkg/shared"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validateUniqueness checks the lists of roles and service IDs for duplicates.
func validateUniqueness(
	appCR *kdv1.KubeDirectorApp,
	allRoleIDs []string,
	allServiceIDs []string,
) string {

	var errorMessages []string
	if !shared.ListIsUnique(allRoleIDs) {
		errorMessages = append(errorMessages, nonUniqueRoleID)
	}
	if !shared.ListIsUnique(allServiceIDs) {
		errorMessages = append(errorMessages, nonUniqueServiceID)
	}

	if len(errorMessages) == 0 {
		return ""
	}
	return strings.Join(errorMessages, "\n")
}

// validateRefUniqueness checks the lists of role references for duplicates.
func validateRefUniqueness(
	appCR *kdv1.KubeDirectorApp,
) string {

	var errorMessages []string
	if !shared.ListIsUnique(appCR.Spec.Config.SelectedRoles) {
		errorMessages = append(errorMessages, nonUniqueSelectedRole)
	}
	roleSeen := make(map[string]bool)
	for _, roleService := range appCR.Spec.Config.RoleServices {
		if _, ok := roleSeen[roleService.RoleID]; ok {
			errorMessages = append(errorMessages, nonUniqueServiceRole)
			break
		}
		roleSeen[roleService.RoleID] = true
	}

	if len(errorMessages) == 0 {
		return ""
	}
	return strings.Join(errorMessages, "\n")
}

// validateServiceRoles checks service_ids and role_id from role_services
// in the config section, to ensure that they refer to legal/existing service
// and role definitions.
func validateServiceRoles(
	appCR *kdv1.KubeDirectorApp,
	allRoleIDs []string,
	allServiceIDs []string,
) string {

	var errorMessages []string
	for _, nodeRole := range appCR.Spec.Config.RoleServices {
		if !shared.StringInList(nodeRole.RoleID, allRoleIDs) {
			invalidMsg := fmt.Sprintf(
				invalidNodeRoleID,
				nodeRole.RoleID,
				strings.Join(allRoleIDs, ","),
			)
			errorMessages = append(errorMessages, invalidMsg)
		}
		for _, serviceID := range nodeRole.ServiceIDs {
			if !shared.StringInList(serviceID, allServiceIDs) {
				invalidMsg := fmt.Sprintf(
					invalidServiceID,
					serviceID,
					strings.Join(allServiceIDs, ","),
				)
				errorMessages = append(errorMessages, invalidMsg)
			}
		}
	}

	if len(errorMessages) == 0 {
		return ""
	}
	return strings.Join(errorMessages, "\n")
}

// validateSelectedRoles checks the selected_roles array to make sure it
// only contains valid role IDs.
func validateSelectedRoles(
	appCR *kdv1.KubeDirectorApp,
	allRoleIDs []string,
) string {

	var errorMessages []string
	for _, role := range appCR.Spec.Config.SelectedRoles {
		if catalog.GetRoleFromID(appCR, role) == nil {
			invalidMsg := fmt.Sprintf(
				invalidSelectedRoleID,
				role,
				strings.Join(allRoleIDs, ","),
			)
			errorMessages = append(errorMessages, invalidMsg)
		}
	}

	if len(errorMessages) == 0 {
		return ""
	}
	return strings.Join(errorMessages, "\n")
}

// validateRoles checks each role for property constraints not expressable
// in the schema. Currently this just means checking that the role must
// specify an image if there is no top-level default image.
func validateRoles(
	appCR *kdv1.KubeDirectorApp,
) string {

	for _, role := range appCR.Spec.NodeRoles {
		if role.Image.RepoTag == "" {
			if appCR.Spec.Image.RepoTag == "" {
				return noDefaultImage
			}
		}
	}
	return ""
}

// validateServices checks each service for property constraints not
// expressable in the schema. Currently this just means checking that the
// service endpoint must specify url_schema if is_dashboard is true.
func validateServices(
	appCR *kdv1.KubeDirectorApp,
) string {

	var errorMessages []string
	for _, service := range appCR.Spec.Services {
		if service.Endpoint.IsDashboard {
			if service.Endpoint.URLScheme == "" {
				invalidMsg := fmt.Sprintf(
					noUrlScheme,
					service.ID,
				)
				errorMessages = append(errorMessages, invalidMsg)
			}
		}
	}

	if len(errorMessages) == 0 {
		return ""
	}
	return strings.Join(errorMessages, "\n")
}

// admitAppCR is the top-level app validation function, which invokes
// the top-specific validation subroutines and composes the admission
// response.
func admitAppCR(
	ar *v1beta1.AdmissionReview,
	handlerState *reconciler.Handler,
) *v1beta1.AdmissionResponse {

	var errorMessages []string

	var admitResponse = v1beta1.AdmissionResponse{
		Allowed: false,
	}

	raw := ar.Request.Object.Raw
	appCR := kdv1.KubeDirectorApp{}

	if err := json.Unmarshal(raw, &appCR); err != nil {
		admitResponse.Result = &metav1.Status{
			Message: "\n" + err.Error(),
		}
		return &admitResponse
	}

	allRoleIDs := catalog.GetAllRoleIDs(&appCR)
	allServiceIDs := catalog.GetAllServiceIDs(&appCR)

	// Verify uniqueness constraints in the roles and services lists.
	uniquenessErr := validateUniqueness(&appCR, allRoleIDs, allServiceIDs)
	if uniquenessErr != "" {
		errorMessages = append(errorMessages, uniquenessErr)
	}

	// Verify uniqueness in the lists of role references in the config section
	// of the app.
	refUniquenessErr := validateRefUniqueness(&appCR)
	if refUniquenessErr != "" {
		errorMessages = append(errorMessages, refUniquenessErr)
	}

	// Verify node services from the config section of the app
	serviceRoleErr := validateServiceRoles(&appCR, allRoleIDs, allServiceIDs)
	if serviceRoleErr != "" {
		errorMessages = append(errorMessages, serviceRoleErr)
	}

	// Verify selected_roles from the config section of the app
	selectedRoleErr := validateSelectedRoles(&appCR, allRoleIDs)
	if selectedRoleErr != "" {
		errorMessages = append(errorMessages, selectedRoleErr)
	}

	// Verify that each role has the required properties.
	rolesErr := validateRoles(&appCR)
	if rolesErr != "" {
		errorMessages = append(errorMessages, rolesErr)
	}

	// Verify that each service has the required properties.
	servicesErr := validateServices(&appCR)
	if servicesErr != "" {
		errorMessages = append(errorMessages, servicesErr)
	}

	if len(errorMessages) == 0 {
		admitResponse.Allowed = true
	} else {
		admitResponse.Result = &metav1.Status{
			Message: "\n" + strings.Join(errorMessages, "\n"),
		}
	}

	return &admitResponse
}
