package client

import (
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

type GCPClient struct {
	Project        string
	ComputeService compute.Service
	Ctx            context.Context
}

type GCPClientInterface interface {
	GetInstanceCreator(instance, zone string) (string, error)
	GetInstanceTemplateName(instance, zone string) (string, error)
	IsTemplateAvailable(instanceTemplate string) (bool, error)
	NeedsUpdate(nodeName string) (bool, error)
	TerminateInstance(instance, zone string) error
}

// In case of a gcp link it returns the target (final part after /)
func formatLinkString(in string) string {

	if strings.ContainsAny(in, "/") {
		elems := strings.Split(in, "/")
		return elems[len(elems)-1]
	}
	return in
}

func NewGCPClient(project string) (*GCPClient, error) {

	ctx := context.Background()

	c, err := google.DefaultClient(ctx, compute.CloudPlatformScope)
	if err != nil {
		return nil, err
	}

	computeService, err := compute.New(c)
	if err != nil {
		return nil, err
	}
	gc := &GCPClient{
		Project:        project,
		ComputeService: *computeService,
		Ctx:            ctx,
	}

	return gc, nil
}

func (gc *GCPClient) GetInstanceCreator(instance, zone string) (string, error) {
	// Get instance object from the api
	resp, err := gc.ComputeService.Instances.Get(gc.Project, zone, instance).Context(gc.Ctx).Do()
	if err != nil {
		return "", err
	}

	meta := resp.Metadata

	var instanceCreator string
	for _, m := range meta.Items {
		if m.Key == "created-by" {
			instanceCreator = *m.Value
		}
	}

	if instanceCreator == "" {
		return "", errors.New("No instance creator found")
	}
	return instanceCreator, nil
}

func (gc *GCPClient) GetInstanceTemplateName(instance, zone string) (string, error) {
	// Get instance object from the api
	resp, err := gc.ComputeService.Instances.Get(gc.Project, zone, instance).Context(gc.Ctx).Do()
	if err != nil {
		return "", err
	}

	meta := resp.Metadata

	// Determine instance template
	var instanceTemplate string
	for _, m := range meta.Items {
		if m.Key == "instance-template" {
			instanceTemplate = *m.Value
		}
	}
	if instanceTemplate == "" {
		return "", errors.New("No instance template found")
	}
	return instanceTemplate, nil
}

func (gc *GCPClient) IsTemplateAvailable(instanceTemplate string) (bool, error) {
	instanceTemplate = formatLinkString(instanceTemplate)
	_, err := gc.ComputeService.InstanceTemplates.Get(gc.Project, instanceTemplate).Context(gc.Ctx).Do()
	if err != nil {
		ae, ok := err.(*googleapi.Error)
		if ok && ae.Code == 404 {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func (gc *GCPClient) NeedsUpdate(nodeName, region, zone string) (bool, error) {
	instanceTemplate, err := gc.GetInstanceTemplateName(nodeName, zone)
	if err != nil {
		return false, err
	}

	instanceCreator, err := gc.GetInstanceCreator(nodeName, zone)
	if err != nil {
		return false, err
	}

	// Let's just assume that the instance was crated by a Regional Group Manager else fail
	groupManager, err := gc.ComputeService.RegionInstanceGroupManagers.Get(gc.Project, region, formatLinkString(instanceCreator)).Context(gc.Ctx).Do()
	if err != nil {
		return false, err
	}

	if groupManager.InstanceTemplate == instanceTemplate {
		return false, nil
	} else {
		return true, nil
	}

	//available, err := gc.IsTemplateAvailable(instanceTemplate)
	//if err != nil {
	//	return false, err
	//}

	//return !available, nil
}

// Terminate instance won't be enough
// Instance needs to be recreated from instance group in order to get the new template
// $ gcloud compute instance-groups managed recreate-instances NAME --instances=INSTANCE
func (gc *GCPClient) TerminateInstance(instance, region, zone string) error {
	//_, err := gc.ComputeService.Instances.Delete(gc.Project, zone, instance).Context(gc.Ctx).Do()
	//if err != nil {
	//	return err
	//}
	//return nil
	inst, err := gc.ComputeService.Instances.Get(gc.Project, zone, instance).Context(gc.Ctx).Do()
	if err != nil {
		return err
	}
	rb := &compute.RegionInstanceGroupManagersRecreateRequest{
		Instances: []string{inst.SelfLink},
	}

	instanceCreator, err := gc.GetInstanceCreator(instance, zone)
	if err != nil {
		return err
	}

	_, err = gc.ComputeService.RegionInstanceGroupManagers.RecreateInstances(gc.Project, region, formatLinkString(instanceCreator), rb).Context(gc.Ctx).Do()
	if err != nil {
		return err
	}
	return nil

}
