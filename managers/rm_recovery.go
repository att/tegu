// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*

	Mnemonic:	rm_recovery
	Abstract:	Functions associated with recovering reservations either from a checkpoint/datacache
				or that had recoverable failures during checkpoint/datacache recovery.
	Date:		22 March 2016  (extracted from res_mgr)
	Author:		E. Scott Daniels


	Mods:
*/

package managers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
	Given a pledge, vet it. Called during checkpoint load, or when running the 
	retry queue. Returns a diposition state:
		DS_ADD 		- Add pledge to reservation cache
		DS_RETRY	- Add to retry queue (recoverable error)
		DS_DISCARD	- Discard it; error but not recoverable
*/
func vet_pledge( p *gizmos.Pledge ) ( disposition int ) {
	var (
		my_ch	chan	*ipc.Chmsg
	)

	if p == nil {
		return DS_DISCARD
	}

	if  (*p).Is_expired() {
		rm_sheep.Baa( 1, "resmgr: ckpt_load: ignored expired pledge: %s", (*p).String() )
		return DS_DISCARD
	} else {
		switch sp := (*p).(type) {									// work on specific pledge type, but pass the Pledge interface to add()
			case *gizmos.Pledge_mirror:
				//err = i.Add_res( p )								// assume we can just add it back in as is

			case *gizmos.Pledge_steer:
				rm_sheep.Baa( 0, "did not restore steering reservation from checkpoint; not implemented" )
				return DS_DISCARD

			case *gizmos.Pledge_bwow:
				h1, h2 := sp.Get_hosts( )							// get the host names, fetch ostack data and update graph
				push_block := h2 == nil
				update_graph( h1, push_block, push_block )			// dig h1 info; push to netmgr if h2 isn't known and block on response
				if h2 != nil {
					update_graph( h2, true, true )					// dig h2 data and push to netmgr blocking for a netmgr response
				}

				my_ch = make( chan *ipc.Chmsg )
				req := ipc.Mk_chmsg( )								// now safe to ask netmgr to validate the oneway pledge
				req.Send_req( nw_ch, my_ch, REQ_BWOW_RESERVE, sp, nil )
				req = <- my_ch										// should be OK, but the underlying network could have changed

				if req.Response_data != nil {
					gate := req.Response_data.( *gizmos.Gate )		// expect that network sent us a gate
					sp.Set_gate( gate )
					rm_sheep.Baa( 1, "gate allocated for oneway reservation: %s %s %s %s", *(sp.Get_id()), *h1, *h2, *(gate.Get_extip()) )
					//err = i.Add_res( p )
				} else {
					rm_sheep.Baa( 0, "WRN: pledge_vet: unable to reserve for oneway pledge: %s	[TGURMG000]", (*p).To_str() )
					return  DS_RETRY
				}

			case *gizmos.Pledge_bw:
				h1, h2 := sp.Get_hosts( )							// get the host names, fetch ostack data and update graph
				update_graph( h1, false, false )					// don't need to block on this one, nor update fqmgr
				update_graph( h2, true, true )						// wait for netmgr to update graph and then push related data to fqmgr

				my_ch = make( chan *ipc.Chmsg )
				req := ipc.Mk_chmsg( )								// now safe to ask netmgr to find a path for the pledge
				rm_sheep.Baa( 2, "reserving path starts" )
				req.Send_req( nw_ch, my_ch, REQ_BW_RESERVE, sp, nil )
				req = <- my_ch										// should be OK, but the underlying network could have changed

				if req.Response_data != nil {
				rm_sheep.Baa( 2, "reserving path finished" )
					path_list := req.Response_data.( []*gizmos.Path )			// path(s) that were found to be suitable for the reservation
					sp.Set_path_list( path_list )
					rm_sheep.Baa( 1, "path allocated for chkptd reservation: %s %s %s; path length= %d", *(sp.Get_id()), *h1, *h2, len( path_list ) )
					//err = i.Add_res( p )
				} else {
					rm_sheep.Baa( 0, "WRN: pledge_vet: unable to reserve for pledge: %s	[TGURMG000]", (*p).To_str() )
					return  DS_RETRY
				}

			case *gizmos.Pledge_pass:
				host, _ := sp.Get_hosts()
				update_graph( host, true, true )

				my_ch = make( chan *ipc.Chmsg )
				req := ipc.Mk_chmsg( )								// now safe to ask netmgr to find a path for the pledge
				req.Send_req( nw_ch, my_ch, REQ_GETPHOST, host, nil )		// need to find the current phost for the vm
				req = <- my_ch

				if req.Response_data != nil {
					phost := req.Response_data.( *string  )
					sp.Set_phost( phost )
					rm_sheep.Baa( 1, "passthrou phost found  for chkptd reservation: %s %s %s", *(sp.Get_id()), *host, *phost )
					//err = i.Add_res( p )
				} else {
					s := fmt.Errorf( "unknown reason" )
					if req.State != nil {
						s = req.State
					}
					rm_sheep.Baa( 0, "WRN: pledge_vet: unable to find phost for passthru pledge: %s	[TGURMG000]", s )
					rm_sheep.Baa( 0, "erroring passthru pledge: %s", (*p).To_str() )
					return  DS_RETRY
				}
				

			default:
				rm_sheep.Baa( 0, "rmgr/vet_pledge: unrecognised pledge type" )
				return DS_DISCARD

		}						// end switch on specific pledge type
	}

	return DS_ADD
}

/*
	Stuff the pledge into the retry cache erroring if the pledge already exists.
	Expect either a Pledge, or a pointer to a pledge.
*/
func (inv *Inventory) Add_retry( pi interface{} ) (err error) {
	var (
		p *gizmos.Pledge
	)

	err = nil

	px, ok := pi.( gizmos.Pledge )
	if ok {
		p = &px
	} else {
		py, ok := pi.( *gizmos.Pledge )
		if ok {
			p = py
		} else {
			err = fmt.Errorf( "internal mishap in Add_res: expected Pledge or *Pledge, got neither" )
			rm_sheep.Baa( 1, "%s", err )
			return
		}
	}

	id := (*p).Get_id()
	if inv.retry[*id] != nil {
		rm_sheep.Baa( 2, "reservation not added to retry cache, already exists: %s", *id )
		err = fmt.Errorf( "reservation already exists in retry cache: %s", *id )
		return
	}

	inv.retry[*id] = p

	rm_sheep.Baa( 1, "resgmgr: added reservation to retry cache: %s", (*p).To_chkpt() )
	return
}

/*
	Opens the filename passed in and reads the reservation data from it. The assumption is
	that records in the file were saved via the write_chkpt() function and are JSON pledges
	or other serializable objects.  We will drop any pledges that expired while 'sitting'
	in the file.
*/
func (inv *Inventory) load_chkpt( fname *string ) ( err error ) {
	var (
		rec		string
		nrecs	int = 0
		p		*gizmos.Pledge
	)

	err = nil

	f, err := os.Open( *fname )
	if err != nil {
		rm_sheep.Baa( 1, "checkpoint open failed for %s: %s", *fname, err )
		return
	}
	defer f.Close( )

	rm_sheep.Baa( 1, "loading from checkpoint: %s", *fname )

	added := 0			// counters for end bleat
	queued := 0
	failed := 0

	br := bufio.NewReader( f )
	for ; err == nil ; {
		rec, err = br.ReadString( '\n' )
		if err == nil  {
			nrecs++

			switch rec[0:5] {
				case "ucap:":
					toks := strings.Split( rec, " " )
					if len( toks ) == 3 {
						inv.add_ulcap( &toks[1], &toks[2] )
					}

				default:
					p, err = gizmos.Json2pledge( &rec )			// convert any type of json pledge to Pledge
					if err == nil {
						switch vet_pledge( p ) {
							case DS_ADD:
								rm_sheep.Baa( 2, "reservaton vetted; added to the cache: %s", (*p).Get_id() )
								err = inv.Add_res( p )				// vet ok, add to reservation cache
								added++

							case DS_RETRY:
								rm_sheep.Baa( 2, "reservaton had recoverable errors; added to retry list: %s", *((*p).Get_id()) )
								inv.Add_retry( p )
								queued++

							default:
								rm_sheep.Baa( 2, "reservaton expired or had unrecoverable errors; discarded: %s", p )
								failed++
						}
					} else {
						rm_sheep.Baa( 0, "CRI: %s", err )
						return			// quickk escape
					}
			}				// outer switch
		}
	}

	if err == io.EOF {
		err = nil
	}

	rm_sheep.Baa( 1, "read %d records from checkpoint file: %s:  %d adds; %d queued for retry; %d dropped", nrecs, *fname, added, queued, failed )
	return
}

/*
	Driven now and again to attempt to push any reservations in the retry cash back into 
	the real world. 
*/
func( inv *Inventory ) vet_retries( ) {
	moved := 0
	tried := 0

	for k, v := range inv.retry {
		tried++

		switch vet_pledge( v ) {
			case DS_ADD:						// pledge can now be supported
				err := inv.Add_res( v )
				if err == nil {
					moved++
					delete( inv.retry, k )			// drop from retry queue
				} else {
					rm_sheep.Baa( 1, "pledge vetted, but unable to add to cache: %s: %s", k, err )
				}

			case DS_DISCARD:					// something didn't work in a non-recoverable way, drop the reserbation
				rm_sheep.Baa( 1, "pledge vetting failed in a non-recoverable way, dropped" )
				delete( inv.retry, k )			// drop from retry queue

			default:							// let it ride
				rm_sheep.Baa( 2, "reservaton had recoverable errors; kept on the retry list: %s", k )
		}
	}

	if tried > 0 {
		rm_sheep.Baa( 1, "attempted to move %d pledges from retry queue, %d successfully moved", tried, moved )
	}
}
