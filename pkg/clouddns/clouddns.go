package clouddns

import (
        "golang.org/x/oauth2"
        "golang.org/x/oauth2/google"
        dns "google.golang.org/api/dns/v1"
)

type managedZonesServiceInterface interface {
        Create(project string, managedzone *dns.ManagedZone) managedZonesCreateCallInterface
        List(project string) managedZonesListCallInterface
}

type changesServiceInterface interface {
        Create(project string, managedZone string, change *dns.Change) changesCreateCallInterface
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

type resourceRecordSetsClientInterface interface {
        List(project string, managedZone string) resourceRecordSetsListCallInterface
}

func NewCloudDNSProvider(project string) (*CloudDNSProvider, error){
        saSecret, err := s.secretLister.Secrets("knative-serving").Get("cloud-dns-key")
        if err != nil {
                return nil, fmt.Errorf("error getting clouddns service account: %s", err)
        }

        saKey := "key.json"
        saBytes := saSecret.Data[saKey]
        if len(saBytes) == 0 {
                return nil, fmt.Errorf("specfied key %q not found in secret %s/%s", saKey, saSecret.Namespace, saSecret.Name)
        }
        impl, err := newDNSProviderServiceAccountCerts(project, saBytes, RecursiveNameservers)
        if err != nil {
                return nil, fmt.Errorf("error instantiating google clouddns challenge solver: %s", err)
        }
}

func newCloudDNSProviderServiceAccountCerts(project string, saBytes []byte) (*DNSProvider, error) {
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
        return &CloudDnsProvider{
                project:                  project,
                managedZonesClient:       managedZonesService{dnsClient.ManagedZones},
                changesClient:            changesService{dnsClient.Changes},
        }, nil
}

func (p *CloudDNSProvider) CreateRecords(endpoints []*Endpoint) error {
        change := &dns.Change{}
        change.Additions = append(change.Additions, p.newFilteredRecords(endpoints)...)
        return p.submitChange(change)
}

// newFilteredRecords returns a collection of RecordSets based on the given endpoints and domainFilter.
func (p *GoogleProvider) newFilteredRecords(endpoints []*Endpoint) []*dns.ResourceRecordSet {
        records := []*dns.ResourceRecordSet{}

        for _, endpoint := range endpoints {
                records = append(records, newRecord(endpoint))
        }

        return records
}

func (p *CloudDNSProvider) submitChange(change *dns.Change) error {
        if len(change.Additions) == 0 && len(change.Deletions) == 0 {
                log.Info("All records are already up to date")
                return nil
        }

        zones, err := p.Zones()
        if err != nil {
                return err
        }

        // separate into per-zone change sets to be passed to the API.
        changes := separateChange(zones, change)

        for z, c := range changes {
                log.Infof("Change zone: %v", z)
                for _, del := range c.Deletions {
                        log.Infof("Del records: %s %s %s %d", del.Name, del.Type, del.Rrdatas, del.Ttl)
                }
                for _, add := range c.Additions {
                        log.Infof("Add records: %s %s %s %d", add.Name, add.Type, add.Rrdatas, add.Ttl)
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
func newRecord(ep *Endpoint) *dns.ResourceRecordSet {
        // TODO(linki): works around appending a trailing dot to TXT records. I think
        // we should go back to storing DNS names with a trailing dot internally. This
        // way we can use it has is here and trim it off if it exists when necessary.
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

