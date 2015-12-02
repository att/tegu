
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

	Mnemonic:	bandwidth.go
	Abstract:	Functions that support bandwidth reservations.

	Date:		19 November 2015	(broken out 02 Dec 2015)
	Author:		E. Scott Daniels

	Mods:
*/

package datacache

import (
	"fmt"
	"time"

	"github.com/att/gopkgs/transform"

	"github.com/gocql/gocql"			// cassandra db interface (requires go 1.4 or later)
)



/*
	Generic reservation save should work for all.
*/
func ( dc *Dcache ) set_reservation( resid string, expiry int64, project string, res interface{}, table string ) ( err error ) {
	if dc == nil {
		return fmt.Errorf( "no struct passed to set_bwres" )
	}

	if resid ==""  {
		return fmt.Errorf( "invalid reservation id" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return err
		}
	}

	if res != nil {
		dc.sheep.Baa( 2, "saving bandwidth reservation in datacache: %s", resid )
		resm := transform.Struct_to_map( res, "dcache" )							// transform the exported datacache fields from struct into a map
    	err = dc.sess.Query( fmt.Sprintf( "INSERT INTO %s (resid, expiry, project, resdata) VALUES (?, ?, ?, ?)", table ), resid, expiry, project, resm ).Exec()
    	if err != nil {
			dc.sheep.Baa( 1, "unable to set bandwidth reservation: key=%s proj=%s err=%s", resid, project, err )
			return err
    	} else {
			dc.sheep.Baa( 1, "reservation successfully added to datacache: resid=%s proj=%s", resid, project )
		}
	} else {
    	err = dc.sess.Query( fmt.Sprintf( "`DELETE FROM %s WHERE resid = ?`", table), resid ).Exec()
		if err != nil {
			dc.sheep.Baa( 1, "unable to delete bandwidth reservation: key=%s err=%s", resid, err )
			return err
		} else {
			dc.sheep.Baa( 1, "bandwidth reservation deleted from datacache: %s", resid )
		}
	}

	return nil
}

/*
	Saves a reservation into the datacache.
	If res is nil, then the reservation is deleted. The entry is keyed on reservation id and the project
	id to make by project listing easier.
*/
func ( dc *Dcache ) Set_bwres( resid string, expiry int64, project string, res interface{} ) ( err error ) {
	return dc.set_reservation( resid, expiry, project, res, "bwres" )
}
/*
	Delete the reservation from the datacache
*/
func ( dc *Dcache ) Del_bwres( resid string ) ( err error ) {
	return dc.set_reservation( resid, 1, "", nil, "bwres" )
}

/*
	Delete all reservations which have expired.
*/
func ( dc *Dcache ) Delex_bwres( ) ( err error ) {
	return nil
}

/*
	Returns a list of reservation IDs that are currently in the datacache.
	This is an internal workhorse as the only difference is the table name.
	Inc_exp will cause expired reservations to be included in the list when
	set to true.
*/
func ( dc *Dcache ) get_res_list( table string, inc_exp bool ) ( rlist []string, err error ) {
	var (
		resid	string
		expiry	int64
	)

	if dc == nil {
		return nil, fmt.Errorf( "no struct passed to get_res_list" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return nil, err
		}
	}

	ecount := 0
	now := time.Now().Unix()
	size := 1024
	rlist = make( []string, 0, size )			// initial cap set at size, it will grow if needed
    iter := dc.sess.Query( fmt.Sprintf( `select  resid,expiry  from %s`, table ) ).Consistency(gocql.One).Iter()
    for iter.Scan( &resid, &expiry ) {
		if inc_exp || expiry > now {
			rlist = append( rlist, resid )			// this will grow if we reach capacity so we must reassign
		} else {
			ecount++
		}
    }

	dc.sheep.Baa( 1, "%d reservations exist in the datacache (%s) %d expired not listed", len( rlist ), table, ecount )
	return rlist[0:len( rlist )], nil
}

/*
	Specific bwres list function.
*/
func ( dc *Dcache ) Get_bwres_list( inc_exp bool ) ( rlist []string, err error ) {
	return dc.get_res_list( "bwres", inc_exp )
}

/*
	Given a bandwidth reservation id, look it up and return the structure with information from 
	the data cache filled in.  Returns error if not found etc.
*/
func ( dc *Dcache ) Get_one_bwres( resid string, target interface{} ) ( err error ) {
	return dc.get_one_map( "bwres", "resid", resid, "resdata", target )
}
