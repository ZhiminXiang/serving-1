package clouddns

import (
        "fmt"
        "context"
        "net"
        "strings"

        "golang.org/x/oauth2"
        "golang.org/x/oauth2/google"
        dns "google.golang.org/api/dns/v1"
        googleapi "google.golang.org/api/googleapi"
        "go.uber.org/zap"
        corev1listers "k8s.io/client-go/listers/core/v1"
)

type managedZonesCreateCallInterface interface {
        Do(opts ...googleapi.CallOption) (*dns.ManagedZone, error)
}

type managedZonesServiceInterface interface {
        Create(project string, managedzone *dns.ManagedZone) managedZonesCreateCallInterface
        List(project string) managedZonesListCallInterface
}

type managedZonesListCallInterface interface {
        Pages(ctx context.Context, f func(*dns.ManagedZonesListResponse) error) error
}

type changesCreateCallInterface interface {
        Do(opts ...googleapi.CallOption) (*dns.Change, error)
}

type changesServiceInterface interface {
        Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface
}

type changesService struct {
        service *dns.ChangesService
}

func (c changesService) Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface {
        return c.service.Create(project, managedZone, change)
}

// Endpoint is a high-level way of a connection between a service and an IP
type Endpoint struct {
        // The hostname of the DNS record
        DNSName string
        // The targets the DNS record points to
        Targets Targets
        // RecordType type of record, e.g. CNAME, A, TXT etc
        RecordType string
        // TTL for the record
        RecordTTL TTL
        // Labels stores labels defined for the Endpoint
        Labels Labels
}

// Labels store metadata related to the endpoint
// it is then stored in a persistent storage via serialization
type Labels map[string]string

type zoneIDName map[string]string

func (z zoneIDName) FindZone(hostname string) (suitableZoneID, suitableZoneName string) {
        for zoneID, zoneName := range z {
                if hostname == zoneName || strings.HasSuffix(hostname, "."+zoneName) {
                        if suitableZoneName == "" || len(zoneName) > len(suitableZoneName) {
                                suitableZoneID = zoneID
                                suitableZoneName = zoneName
                        }
                }
        }
        return
}

// NewLabels returns empty Labels
func NewLabels() Labels {
        return map[string]string{}
}

// Targets is a representation of a list of targets for an endpoint.
type Targets []string

// NewTargets is a convenience method to create a new Targets object from a vararg of strings
func NewTargets(target ...string) Targets {
        t := make(Targets, 0, len(target))
        t = append(t, target...)
        return t
}

type TTL int64

// IsConfigured returns true if TTL is configured, false otherwise
func (ttl TTL) IsConfigured() bool {
        return ttl > 0
}

// GoogleProvider is an implementation of Provider for Google CloudDNS.
type CloudDNSProvider struct {
        // The Google project to work in
        project string
        // A client for managing hosted zones
        managedZonesClient managedZonesServiceInterface
        // A client for managing change sets
        changesClient changesServiceInterface
}

type managedZonesService struct {
        service *dns.ManagedZonesService
}

func (m managedZonesService) Create(project string, managedzone *dns.ManagedZone) managedZonesCreateCallInterface {
        return m.service.Create(project, managedzone)
}

func (m managedZonesService) List(project string) managedZonesListCallInterface {
        return m.service.List(project)
}

func NewCloudDNSProvider(project string, secretLister corev1listers.SecretLister) (*CloudDNSProvider, error){
        saSecret, err := secretLister.Secrets("knative-serving").Get("cloud-dns-key")
        if err != nil {
                return nil, fmt.Errorf("Error to get Cloud DNS Key: %v", err)
        }

        saKey := "key.json"
        saBytes := saSecret.Data[saKey]
        if len(saBytes) == 0 {
                return nil, fmt.Errorf("specfied key %q not found in secret %s/%s", saKey, saSecret.Namespace, saSecret.Name)
        }
        impl, err := newCloudDNSProviderServiceAccountCerts(project, saBytes)
        if err != nil {
                return nil, fmt.Errorf("error instantiating google clouddns challenge solver: %s", err)
        }
        return impl, err
}

func newCloudDNSProviderServiceAccountCerts(project string, saBytes []byte) (*CloudDNSProvider, error) {
        if project == "" {
                return nil, fmt.Errorf("Google Cloud project name missing")
        }
        if len(saBytes) == 0 {
                return nil, fmt.Errorf("Google Cloud Service Account certs missing")
        }

        conf, err := google.JWTConfigFromJSON(saBytes, dns.NdevClouddnsReadwriteScope)
        if err != nil {
                return nil, fmt.Errorf("Unable to acquire config: %v", err)
        }
        client := conf.Client(oauth2.NoContext)

        dnsClient, err := dns.New(client)
        if err != nil {
                return nil, fmt.Errorf("Unable to create Google Cloud DNS service: %v", err)
        }
        return &CloudDNSProvider{
                project:                  project,
                managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
                changesClient:            changesService{dnsClient.Changes},
        }, nil
}

func (p *CloudDNSProvider) CreateRecords(endpoints []*Endpoint, logger *zap.SugaredLogger) error {
        change := &dns.Change{}
        change.Additions = append(change.Additions, p.newFilteredRecords(endpoints, logger)...)
        return p.submitChange(change, logger)
}

// newFilteredRecords returns a collection of RecordSets based on the given endpoints and domainFilter.
func (p *CloudDNSProvider) newFilteredRecords(endpoints []*Endpoint, logger *zap.SugaredLogger) []*dns.ResourceRecordSet {
        records := []*dns.ResourceRecordSet{}

        for _, endpoint := range endpoints {
                records = append(records, newRecord(endpoint, logger))
        }

        return records
}

// Zones returns the list of hosted zones.
func (p *CloudDNSProvider) Zones(logger *zap.SugaredLogger) (map[string]*dns.ManagedZone, error) {
        zones := make(map[string]*dns.ManagedZone)

        f := func(resp *dns.ManagedZonesListResponse) error {
                for _, zone := range resp.ManagedZones {
                        zones[zone.Name] = zone
                }
                return nil
        }

        if err := p.managedZonesClient.List(p.project).Pages(context.TODO(), f); err != nil {
                return nil, err
        }

        if len(zones) == 0 {
                logger.Warnf("No matched zones found in the project, %s", p.project)
        }

        for _, zone := range zones {
                logger.Debugf("Considering zone: %s (domain: %s)", zone.Name, zone.DnsName)
        }

        return zones, nil
}

func (p *CloudDNSProvider) submitChange(change *dns.Change, logger *zap.SugaredLogger) error {
        if len(change.Additions) == 0 && len(change.Deletions) == 0 {
                logger.Info("All records are already up to date")
                return nil
        }

        zones, err := p.Zones(logger)
        if err != nil {
                return err
        }

        // separate into per-zone change sets to be passed to the API.
        changes := p.separateChange(zones, change, logger)

        for z, c := range changes {
                logger.Infof("Change zone: %v", z)
                for _, del := range c.Deletions {
                        logger.Infof("Del records: %s %s %s %d", del.Name, del.Type, del.Rrdatas, del.Ttl)
                }
                for _, add := range c.Additions {
                        logger.Infof("Add records: %s %s %s %d", add.Name, add.Type, add.Rrdatas, add.Ttl)
                }
        }

        for z, c := range changes {
                if _, err := p.changesClient.Create(p.project, z, c).Do(); err != nil {
                        return err
                }
        }

        return nil
}

// newRecord returns a RecordSet based on the given endpoint.
func newRecord(ep *Endpoint, logger *zap.SugaredLogger) *dns.ResourceRecordSet {
        // TODO(linki): works around appending a trailing dot to TXT records. I think
        // we should go back to storing DNS names with a trailing dot internally. This
        // way we can use it has is here and trim it off if it exists when necessary.
        logger.Infof("newRecord %s", *ep)
        targets := make([]string, len(ep.Targets))
        copy(targets, []string(ep.Targets))
        if ep.RecordType == "CNAME" {
                targets[0] = ensureTrailingDot(targets[0])
        }

        // no annotation results in a Ttl of 0, default to 300 for backwards-compatability
        var ttl int64 = 300
        if ep.RecordTTL.IsConfigured() {
                ttl = int64(ep.RecordTTL)
        }

        return &dns.ResourceRecordSet{
                Name:    ensureTrailingDot(ep.DNSName),
                Rrdatas: targets,
                Ttl:     ttl,
                Type:    ep.RecordType,
        }
}

// ensureTrailingDot ensures that the hostname receives a trailing dot if it hasn't already.
func ensureTrailingDot(hostname string) string {
        if net.ParseIP(hostname) != nil {
                return hostname
        }

        return strings.TrimSuffix(hostname, ".") + "."
}

// separateChange separates a multi-zone change into a single change per zone.
func (p *CloudDNSProvider) separateChange(zones map[string]*dns.ManagedZone, change *dns.Change, logger *zap.SugaredLogger) map[string]*dns.Change {
        changes := make(map[string]*dns.Change)
        zoneNameIDMapper := zoneIDName{}
        for _, z := range zones {
                zoneNameIDMapper[z.Name] = z.DnsName
                changes[z.Name] = &dns.Change{
                        Additions: []*dns.ResourceRecordSet{},
                        Deletions: []*dns.ResourceRecordSet{},
                }
        }
        for _, a := range change.Additions {
                if zoneName, _ := zoneNameIDMapper.FindZone(ensureTrailingDot(a.Name)); zoneName != "" {
                        changes[zoneName].Additions = append(changes[zoneName].Additions, a)
                } else {
                        logger.Warnf("No matching zone for record addition: %s %s %s %d", a.Name, a.Type, a.Rrdatas, a.Ttl)
                }
        }

        for _, d := range change.Deletions {
                if zoneName, _ := zoneNameIDMapper.FindZone(ensureTrailingDot(d.Name)); zoneName != "" {
                        changes[zoneName].Deletions = append(changes[zoneName].Deletions, d)
                } else {
                        logger.Warnf("No matching zone for record deletion: %s %s %s %d", d.Name, d.Type, d.Rrdatas, d.Ttl)
                }
        }

        // separating a change could lead to empty sub changes, remove them here.
        for zone, change := range changes {
                if len(change.Additions) == 0 && len(change.Deletions) == 0 {
                        delete(changes, zone)
                }
        }

        return changes
}
