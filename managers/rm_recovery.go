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
		DS_DROP		- Discard it; error but not recoverable
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
					rm_sheep.Baa( 0, "ERR: resmgr: ckpt_laod: unable to reserve for oneway pledge: %s	[TGURMG000]", (*p).To_str() )
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
					rm_sheep.Baa( 0, "ERR: resmgr: ckpt_laod: unable to reserve for pledge: %s	[TGURMG000]", (*p).To_str() )
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
					rm_sheep.Baa( 0, "ERR: resmgr: ckpt_laod: unable to find phost for passthru pledge: %s	[TGURMG000]", s )
					rm_sheep.Baa( 0, "erroring passthru pledge: %s", (*p).To_str() )
					return  DS_RETRY
				}
				

			default:
				rm_sheep.Baa( 0, "rmgr/load_ckpt: unrecognised pledge type" )
				return DS_DISCARD

		}						// end switch on specific pledge type
	}

	return DS_ADD
}

/*
	Opens the filename passed in and reads the reservation data from it. The assumption is
	that records in the file were saved via the write_chkpt() function and are JSON pledges
	or other serializable objects.  We will drop any pledges that expired while 'sitting'
	in the file.
*/
func (i *Inventory) load_chkpt( fname *string ) ( err error ) {
	var (
		rec		string
		nrecs	int = 0
		p		*gizmos.Pledge
		//my_ch	chan	*ipc.Chmsg
		//req		*ipc.Chmsg
	)

	err = nil
	//my_ch = make( chan *ipc.Chmsg )

	f, err := os.Open( *fname )
	if err != nil {
		return
	}
	defer f.Close( )

	br := bufio.NewReader( f )
	for ; err == nil ; {
		rec, err = br.ReadString( '\n' )
		if err == nil  {
			nrecs++

			switch rec[0:5] {
				case "ucap:":
					toks := strings.Split( rec, " " )
					if len( toks ) == 3 {
						i.add_ulcap( &toks[1], &toks[2] )
					}

				default:
					p, err = gizmos.Json2pledge( &rec )			// convert any type of json pledge to Pledge
					if err == nil {
						switch vet_pledge( p ) {
							case DS_ADD:
								err = i.Add_res( p )				// vet ok, add to reservation cache

							case DS_RETRY:
								rm_sheep.Baa( 2, "reservaton had recoverable errors; added to retry list: %s",(*p).Get_id() )
// TODO -- ad to fail list if error

							default:
								rm_sheep.Baa( 2, "reservaton had unrecoverable errors; discarded: %s", p )
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

	rm_sheep.Baa( 1, "read %d records from checkpoint file: %s", nrecs, *fname )
	return
}
