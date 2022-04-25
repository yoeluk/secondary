## Secondary for Authoritative DNS Server

This CoreDNS plugin extends an authoritative DNS plugin to support zone transfer and act as secondary. The design of 
this plugin has in mind resolving a few specific problems. 

* For example, when this secondary plugin gets the NOTIFY message it doesn't assume that the primary is 
at the `sourceIp` of the message, instead it is configured with one or more known primaries and the NOTIFY message 
triggers the search for the changed zone among its known primaries servers. This allow the plugin to work even 
  when the server is behind a DNS forwarder, NLB or VPC Endpoint where the `sourceIp` might not be the 
  client/primary ip address. 
* Since this plugin is meant to extend an authoritative plugin it doesn't itself persist the zone but instead 
  defines a `TransferPersistence` interface and requires that there is at least one plugin that implements it and 
  uses their implementation to read and write to the backend.

## Configuration

This plugin doesn't server dns other than the NOTIFY message as such make sure that you linked it before your 
authoritative plugin which will be serving queries.

Say that you have an authoritative plugin that uses `redis` as the backend. Make sure that the `redis` plugin 
implements the `TransferPersistence` interface.

```go
type TransferPersistence interface {
	Name() string
	Persist(zone string, records []dns.RR) error
	RetrieveSOA(zoneName string) *dns.SOA
}
```

A typical configuration would then look like this.

```go
. {
    health :8080
    secondary {
      primary bind9.primary.svc.cluster.local:53
    }
    redis {
      write_address redis-master.redis.svc.cluster.local:6379
      read_address redis-replicas.redis.svc.cluster.local:6379
      password {{ .Values.redis_password }}
      connect_timeout 2000
      read_timeout 2000
      ttl 300
    }
    debug
    log
}
```