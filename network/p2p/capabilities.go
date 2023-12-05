// Copyright (C) 2019-2023 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package p2p

import (
	"context"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libpeerstore "github.com/libp2p/go-libp2p/core/peerstore"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/logging"
	algoDht "github.com/algorand/go-algorand/network/p2p/dht"
	"github.com/algorand/go-algorand/network/p2p/peerstore"
)

// Capability represents functions that some nodes may provide and other nodes would want to know about
type Capability string

const (
	// Archival nodes
	Archival Capability = "archival"
	// Catchpoints storing nodes
	Catchpoints = "catchpointStoring"
)

const operationTimeout = time.Second * 5
const maxAdvertisementInterval = time.Hour * 22

// CapabilitiesDiscovery exposes Discovery interfaces and wraps underlying DHT methods to provide capabilities advertisement for the node
type CapabilitiesDiscovery struct {
	disc discovery.Discovery
	dht  *dht.IpfsDHT
	log  logging.Logger
	wg   sync.WaitGroup
}

// Advertise implements the discovery.Discovery/discovery.Advertiser interface
func (c *CapabilitiesDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	return c.disc.Advertise(ctx, ns, opts...)
}

// FindPeers implements the discovery.Discovery/discovery.Discoverer interface
func (c *CapabilitiesDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error) {
	return c.disc.FindPeers(ctx, ns, opts...)
}

// Close should be called when fully shutting down the node
func (c *CapabilitiesDiscovery) Close() {
	_ = c.dht.Close()
	c.wg.Wait()
}

// Host exposes the underlying libp2p host.Host object
func (c *CapabilitiesDiscovery) Host() host.Host {
	return c.dht.Host()
}

// AddPeer adds a given peer.AddrInfo to the Host's Peerstore, and the DHT's routing table
func (c *CapabilitiesDiscovery) AddPeer(p peer.AddrInfo) (bool, error) {
	c.Host().Peerstore().AddAddrs(p.ID, p.Addrs, libpeerstore.AddressTTL)
	return c.dht.RoutingTable().TryAddPeer(p.ID, true, true)
}

// PeersForCapability returns a slice of peer.AddrInfo for a Capability
// Since CapabilitiesDiscovery uses a backoffcache, it will attempt to hit cache, then disk, then network
// in order to fetch n peers which are advertising the required capability.
func (c *CapabilitiesDiscovery) PeersForCapability(capability Capability, n int) ([]peer.AddrInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	var peers []peer.AddrInfo
	peersChan, err := c.FindPeers(ctx, string(capability), discovery.Limit(n))
	if err != nil {
		return nil, err
	}
	for p := range peersChan {
		if p.ID.Size() > 0 && p.ID != c.Host().ID() {
			peers = append(peers, p)
		}
		if len(peers) >= n {
			break
		}
	}
	return peers, nil
}

// AdvertiseCapabilities periodically runs the Advertiser interface on the DHT
// If a capability fails to advertise we will retry every 10 seconds until full success
// This gets rerun every at the minimum ttl or the maxAdvertisementInterval.
func (c *CapabilitiesDiscovery) AdvertiseCapabilities(capabilities ...Capability) {
	c.wg.Add(1)
	go func() {
		// Run the initial Advertisement immediately
		nextExecution := time.After(time.Second / 10000)
		defer func() {
			c.wg.Done()
		}()

		for {
			select {
			case <-c.dht.Context().Done():
				return
			case <-nextExecution:
				var err error
				advertisementInterval := maxAdvertisementInterval
				for _, capa := range capabilities {
					ttl, err0 := c.Advertise(c.dht.Context(), string(capa))
					if err0 != nil {
						err = err0
						c.log.Errorf("failed to advertise for capability %s: %v", capa, err0)
						break
					}
					if ttl < advertisementInterval {
						advertisementInterval = ttl
					}
					c.log.Infof("advertised capability %s", capa)
				}
				// If we failed to advertise, retry every 10 seconds until successful
				if err != nil {
					nextExecution = time.After(time.Second * 10)
				} else {
					// Otherwise, ensure we're at the correct interval
					nextExecution = time.After(advertisementInterval)
				}
			}
		}
	}()
}

// MakeCapabilitiesDiscovery creates a new CapabilitiesDiscovery object which exposes peer discovery and capabilities advertisement
func MakeCapabilitiesDiscovery(ctx context.Context, cfg config.Local, datadir string, network string, log logging.Logger, phonebookAddresses []string) (*CapabilitiesDiscovery, error) {
	var bootstrapPeers []*peer.AddrInfo
	if len(phonebookAddresses) > 0 {
		var malformedAddrs map[string]string
		bootstrapPeers, malformedAddrs = peerstore.PeerInfoFromAddrs(phonebookAddresses)
		for malAddr, malErr := range malformedAddrs {
			log.Infof("Ignoring malformed phonebook address %s: %s", malAddr, malErr)
		}
	}
	pstore, err := peerstore.NewPeerStore(bootstrapPeers)
	if err != nil {
		return nil, err
	}
	h, err := makeHost(cfg, datadir, pstore)
	if err != nil {
		return nil, err
	}
	discDht, err := algoDht.MakeDHT(ctx, h, network, cfg, bootstrapPeers)
	if err != nil {
		return nil, err
	}
	discImpl, err := algoDht.MakeDiscovery(discDht)
	if err != nil {
		return nil, err
	}
	return &CapabilitiesDiscovery{
		disc: discImpl,
		dht:  discDht,
		log:  log,
	}, nil
}
