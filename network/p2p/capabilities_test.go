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
	"math/rand"
	"sync"
	"testing"
	"time"

	golog "github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/logging"
	algodht "github.com/algorand/go-algorand/network/p2p/dht"
	"github.com/algorand/go-algorand/network/p2p/peerstore"
	"github.com/algorand/go-algorand/test/partitiontest"
)

func TestCapabilities_Discovery(t *testing.T) {
	partitiontest.PartitionTest(t)

	golog.SetDebugLogging()
	var caps []*CapabilitiesDiscovery
	var addrs []peer.AddrInfo
	testSize := 3
	for i := 0; i < testSize; i++ {
		tempdir := t.TempDir()
		capD, err := MakeCapabilitiesDiscovery(context.Background(), config.GetDefaultLocal(), tempdir, "devtestnet", logging.Base(), []*peer.AddrInfo{})
		require.NoError(t, err)
		caps = append(caps, capD)
		addrs = append(addrs, peer.AddrInfo{
			ID:    capD.Host().ID(),
			Addrs: capD.Host().Addrs(),
		})
	}
	for _, capD := range caps {
		peersAdded := 0
		for _, addr := range addrs {
			added, err := capD.AddPeer(addr)
			require.NoError(t, err)
			require.True(t, added)
			peersAdded++
		}
		err := capD.dht.Bootstrap(context.Background())
		require.NoError(t, err)
		capD.dht.ForceRefresh()
		require.Equal(t, peersAdded, capD.dht.RoutingTable().Size())
	}
}

func setupDHTHosts(t *testing.T, numHosts int) []*dht.IpfsDHT {
	var hosts []host.Host
	var bootstrapPeers []*peer.AddrInfo
	var dhts []*dht.IpfsDHT
	cfg := config.GetDefaultLocal()
	for i := 0; i < numHosts; i++ {
		tmpdir := t.TempDir()
		pk, err := GetPrivKey(cfg, tmpdir)
		require.NoError(t, err)
		ps, err := peerstore.NewPeerStore([]*peer.AddrInfo{})
		require.NoError(t, err)
		h, err := libp2p.New(
			libp2p.ListenAddrStrings("/dns4/localhost/tcp/0"),
			libp2p.Identity(pk),
			libp2p.Peerstore(ps))
		require.NoError(t, err)
		hosts = append(hosts, h)
		bootstrapPeers = append(bootstrapPeers, &peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()})
	}
	for _, h := range hosts {
		ht, err := algodht.MakeDHT(context.Background(), h, "devtestnet", cfg, bootstrapPeers)
		require.NoError(t, err)
		err = ht.Bootstrap(context.Background())
		require.NoError(t, err)
		dhts = append(dhts, ht)
	}
	return dhts
}

func waitForRouting(t *testing.T, disc *CapabilitiesDiscovery) {
	refreshCtx, refCancel := context.WithTimeout(context.Background(), time.Second*5)
	for {
		select {
		case <-refreshCtx.Done():
			refCancel()
			require.Fail(t, "failed to populate routing table before timeout")
		default:
			if disc.dht.RoutingTable().Size() > 0 {
				refCancel()
				return
			}
		}
	}
}

func createHosts(t *testing.T, numHosts int, cfg config.Local) []host.Host {
	var hosts []host.Host
	for i := 0; i < numHosts; i++ {
		tmpdir := t.TempDir()
		pk, err := GetPrivKey(cfg, tmpdir)
		require.NoError(t, err)
		ps, err := peerstore.NewPeerStore([]*peer.AddrInfo{})
		require.NoError(t, err)
		h, err := libp2p.New(
			libp2p.ListenAddrStrings("/dns4/localhost/tcp/0"),
			libp2p.Identity(pk),
			libp2p.Peerstore(ps))
		require.NoError(t, err)
		hosts = append(hosts, h)
	}
	return hosts
}

func setupCapDiscovery(t *testing.T, numHosts int, numBootstrapPeers int) []*CapabilitiesDiscovery {
	var capsDisc []*CapabilitiesDiscovery
	cfg := config.GetDefaultLocal()

	hosts := createHosts(t, numHosts, cfg)
	var bootstrapPeers []*peer.AddrInfo
	for _, h := range hosts {
		bootstrapPeers = append(bootstrapPeers, &peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()})
	}

	for _, h := range hosts {
		bp := bootstrapPeers
		if numBootstrapPeers != 0 && numBootstrapPeers != numHosts {
			rand.Shuffle(len(bootstrapPeers), func(i, j int) {
				bp[i], bp[j] = bp[j], bp[i]
			})
			bp = bp[:numBootstrapPeers]
		}
		ht, err := algodht.MakeDHT(context.Background(), h, "devtestnet", cfg, bp)
		require.NoError(t, err)
		disc, err := algodht.MakeDiscovery(ht)
		require.NoError(t, err)
		cd := &CapabilitiesDiscovery{
			disc: disc,
			dht:  ht,
			log:  logging.Base(),
		}
		capsDisc = append(capsDisc, cd)
	}
	return capsDisc
}

func TestCapabilities_DHTTwoPeers(t *testing.T) {
	partitiontest.PartitionTest(t)

	numAdvertisers := 2
	dhts := setupDHTHosts(t, numAdvertisers)
	topic := "foobar"
	for i, ht := range dhts {
		disc, err := algodht.MakeDiscovery(ht)
		require.NoError(t, err)
		refreshCtx, refCancel := context.WithTimeout(context.Background(), time.Second*5)
	peersPopulated:
		for {
			select {
			case <-refreshCtx.Done():
				refCancel()
				require.Fail(t, "failed to populate routing table before timeout")
			default:
				if ht.RoutingTable().Size() > 0 {
					refCancel()
					break peersPopulated
				}
			}
		}
		_, err = disc.Advertise(context.Background(), topic)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		var advertisers []peer.AddrInfo
		peersChan, err := disc.FindPeers(ctx, topic, discovery.Limit(numAdvertisers))
		require.NoError(t, err)
		for p := range peersChan {
			if p.ID.Size() > 0 {
				advertisers = append(advertisers, p)
			}
		}
		cancel()
		// Returned peers will include the querying node's ID since it advertises for the topic as well
		require.Equal(t, i+1, len(advertisers))
	}
}

func TestCapabilities_Varying(t *testing.T) {
	partitiontest.PartitionTest(t)

	const numAdvertisers = 10

	var tests = []struct {
		name         string
		numBootstrap int
	}{
		{"bootstrap=all", numAdvertisers},
		{"bootstrap=2", 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capsDisc := setupCapDiscovery(t, numAdvertisers, test.numBootstrap)
			noCap := capsDisc[:3]
			archOnly := capsDisc[3:5]
			catchOnly := capsDisc[5:7]
			archCatch := capsDisc[7:]

			var wg sync.WaitGroup
			wg.Add(len(archOnly) + len(catchOnly) + len(archCatch))
			for _, disc := range archOnly {
				go func(disc *CapabilitiesDiscovery) {
					defer wg.Done()
					waitForRouting(t, disc)
					disc.AdvertiseCapabilities(Archival)
				}(disc)
			}
			for _, disc := range catchOnly {
				go func(disc *CapabilitiesDiscovery) {
					defer wg.Done()
					waitForRouting(t, disc)
					disc.AdvertiseCapabilities(Catchpoints)
				}(disc)
			}
			for _, disc := range archCatch {
				go func(disc *CapabilitiesDiscovery) {
					defer wg.Done()
					waitForRouting(t, disc)
					disc.AdvertiseCapabilities(Archival, Catchpoints)
				}(disc)
			}

			wg.Wait()

			wg.Add(len(noCap) * 2)
			for _, disc := range noCap {
				go func(disc *CapabilitiesDiscovery) {
					defer wg.Done()
					require.Eventuallyf(t,
						func() bool {
							numArchPeers := len(archOnly) + len(archCatch)
							peers, err := disc.PeersForCapability(Archival, numArchPeers)
							if err == nil && len(peers) == numArchPeers {
								return true
							}
							return false
						},
						time.Minute,
						time.Second,
						"Not all expected archival peers were found",
					)

					// ensure all nodes are discovered and stored in the peer store
					h := disc.dht.Host()
					ps := h.Peerstore()
					require.Equal(t, ps.Peers().Len(), numAdvertisers)
				}(disc)

				go func(disc *CapabilitiesDiscovery) {
					defer wg.Done()
					require.Eventuallyf(t,
						func() bool {
							numCatchPeers := len(catchOnly) + len(archCatch)
							peers, err := disc.PeersForCapability(Catchpoints, numCatchPeers)
							if err == nil && len(peers) == numCatchPeers {
								return true
							}
							return false
						},
						time.Minute,
						time.Second,
						"Not all expected catchpoint peers were found",
					)
				}(disc)
			}

			wg.Wait()

			for _, disc := range capsDisc[3:] {
				disc.Close()
				// Make sure it actually closes
				disc.wg.Wait()
			}
		})
	}
}

func TestCapabilities_ExcludesSelf(t *testing.T) {
	partitiontest.PartitionTest(t)
	disc := setupCapDiscovery(t, 2, 2)

	testPeersFound := func(disc *CapabilitiesDiscovery, n int, cap Capability) bool {
		peers, err := disc.PeersForCapability(cap, n+1)
		if err == nil && len(peers) == n {
			return true
		}
		return false
	}

	waitForRouting(t, disc[0])
	disc[0].AdvertiseCapabilities(Archival)
	// disc[1] finds Archival
	require.Eventuallyf(t,
		func() bool { return testPeersFound(disc[1], 1, Archival) },
		time.Minute,
		time.Second,
		"Could not find archival peer",
	)

	// disc[0] doesn't find itself
	require.Neverf(t,
		func() bool { return testPeersFound(disc[0], 1, Archival) },
		time.Second*5,
		time.Second,
		"Found self when searching for capability",
	)

	disc[0].Close()
	disc[0].wg.Wait()
}

// TestCapabilities_RoutingConnectivity creates a dumbbell-like topology of 6 nodes
// with the "handle" nodes connected to each other, and "weights" nodes connected only its "handle" side.
// Left side nodes are DHT clients and right side nodes are DHT servers one of them announces Archival capability.
// The test checks that left side discovers capablity on the right.
// It also asserts that despite the fact left nodes bootstrapped to the left handle node, participating in DHT they connect
// to other (right side) DHT servers.
func TestCapabilities_RoutingConnectivity(t *testing.T) {
	partitiontest.PartitionTest(t)

	// golog.SetDebugLogging()

	cfg := config.GetDefaultLocal()
	hosts := createHosts(t, 6, cfg)

	leftNodes := hosts[0:2]
	handleNodes := hosts[2:4]
	rightNodes := hosts[4:]
	require.Equal(t, 2, len(leftNodes))
	require.Equal(t, len(leftNodes), len(handleNodes))
	require.Equal(t, len(handleNodes), len(rightNodes))

	toBootstrap := func(hs ...host.Host) []*peer.AddrInfo {
		ai := make([]*peer.AddrInfo, len(hs))
		for i, h := range hs {
			ai[i] = &peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
		}
		return ai
	}

	toCds := func(bootstrap host.Host, mode dht.ModeOpt, hs ...host.Host) []*CapabilitiesDiscovery {
		cds := make([]*CapabilitiesDiscovery, len(hs))
		for i, h := range hs {
			ht, err := algodht.MakeDHTEx(context.Background(), h, "devtestnet", cfg, toBootstrap(bootstrap), mode)
			require.NoError(t, err)
			disc, err := algodht.MakeDiscovery(ht)
			require.NoError(t, err)
			cds[i] = &CapabilitiesDiscovery{
				disc: disc,
				dht:  ht,
				log:  logging.Base(),
			}
		}
		return cds
	}

	// connect left to handle
	leftCd := toCds(handleNodes[0], dht.ModeClient, leftNodes...)
	// connect right to handle
	rightCd := toCds(handleNodes[1], dht.ModeServer, rightNodes...)
	// connect both handle sides
	handleCd := make([]*CapabilitiesDiscovery, len(handleNodes))
	handleCd[0] = toCds(handleNodes[1], dht.ModeServer, handleNodes[0])[0]
	handleCd[1] = toCds(handleNodes[0], dht.ModeServer, handleNodes[1])[0]

	waitForRouting(t, leftCd[0])
	waitForRouting(t, rightCd[0])
	rightCd[0].AdvertiseCapabilities(Archival)

	require.Eventuallyf(t,
		func() bool {
			numArchPeers := 1
			peers, err := leftCd[0].PeersForCapability(Archival, numArchPeers)
			if err == nil && len(peers) == numArchPeers {
				return true
			}
			return false
		},
		time.Minute,
		time.Second,
		"Not all expected archival peers were found",
	)

	// TODO: figure out why Peerstore has self
	require.Equal(t, len(handleNodes)+len(rightNodes)+1, len(leftNodes[0].Peerstore().Peers()))
	require.Contains(t, leftNodes[0].Peerstore().Peers(), leftNodes[0].ID())
	require.NotContains(t, leftNodes[0].Peerstore().Peers(), leftNodes[1].ID())
	require.Equal(t, len(handleNodes)+len(rightNodes), leftCd[0].dht.RoutingTable().Size())

	require.Equal(t, len(handleNodes)+len(rightNodes)+len(leftNodes), len(rightNodes[0].Peerstore().Peers()))
	require.Equal(t, len(handleNodes)+len(rightNodes)-1, rightCd[0].dht.RoutingTable().Size()) // exlcuding self
}
