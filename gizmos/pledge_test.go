// vi: sw=4 ts=4:

/*

	Mnemonic:	pledge_test
	Abstract:	test functions that test various components of pledge and pledge window.
	Date:		01 June 2015
	Author:		E. Scott Daniels

*/

package gizmos

import (
	"fmt"
	"os"
	"testing"
	"time"

)

/*
	make a window and show error if it does
*/
func new_pw( c int64, e int64 ) ( *pledge_window ) {

	p, err := mk_pledge_window( c, e )
	if err != nil {
		fmt.Fprintf( os.Stderr, "ERROR:  did not create pledge window: %s\n", err )
	}

	return p
}


func Test_pwo( t *testing.T ) {
	failures :=0
	now := time.Now().Unix()
	p1_start := now + 3600				// ensure all start times are in future so window creates
	p1_end := now + 7200
	p1 := new_pw( p1_start, p1_end )					// master window to compare others with

	p2 := new_pw( p1_start - 600, p1_start - 300 )		// completely before p1 (expect false)
	p3 := new_pw( p1_end + 800, p1_end + 900 )			// completely after p1 (expect false)
	p4 := new_pw( p1_start + 300, p1_end + 800 )		// commence overlap		(expect true)
	p5 := new_pw( p1_start - 800, p1_start + 300 )		// expiry overlap		(expect true)
	p6 := new_pw( p1_start - 300, p1_end + 800 )		// completely engulf	(expect true)
	p7 := new_pw( p1_start -300 , p1_start )			// ending exactly where p1 starts (expect false)

	fmt.Fprintf( os.Stderr, "\n------- pledge window tests ---------\n" );
	if p1.overlaps( p2 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 reports overlap with pwindow 2\n" )
		failures++
	}

	if p1.overlaps( p3 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 reports overlap with pwindow 3\n" )
		failures++
	}

	// p4-6 do overlap and so failure if it returns false
	if !p1.overlaps( p4 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 does not report overlap with pwindow 4\n" )
		failures++
	}

	if !p1.overlaps( p5 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 does not report overlap with pwindow 5\n" )
		failures++
	}

	if !p1.overlaps( p6 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 does not report overlap with pwindow 6\n" )
		failures++
	}

	// end time is same as p1 start time expect false
	if p1.overlaps( p7 ) {
		fmt.Fprintf( os.Stderr, "FAIL:  pwindow1 reports overlap with pwindow 7\n" )
		failures++
	}

	if failures == 0 {
		fmt.Fprintf( os.Stderr, "OK:     all pledge window overlap tests pass\n" )
	} else {
		t.Fail()
	}

	fmt.Fprintf( os.Stderr, "\n" )
}

/*
*/
func Test_ob_validtime( t *testing.T ) {
	fmt.Fprintf( os.Stderr, "\n------- valid obligattion tests ---------\n" );

	if Valid_obtime( 1735707600-1 ) { 				// expect pass, time just under bounds
		fmt.Fprintf( os.Stderr, "OK:     max-1 time returned valid\n" )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL:   max-1 time didn't return valid\n" )
		t.Fail()
	}

	if Valid_obtime( time.Now().Unix() + 1 ) { 				//expect pass, time just under bounds
		fmt.Fprintf( os.Stderr, "OK:     now+1 time returned valid\n" )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL:   now+1 time didn't return valid\n" )
		t.Fail()
	}

	if Valid_obtime( 1735707600+1 ) {			// expect failure, time out of bounds
		fmt.Fprintf( os.Stderr, "FAIL:   max+1 time returned valid\n" )
		t.Fail()
	} else {
		fmt.Fprintf( os.Stderr, "OK:     max+1 time returned invalid\n" )
	}

	if Valid_obtime( time.Now().Unix() - 1 ) {			// expect failure, time out of bounds
		fmt.Fprintf( os.Stderr, "FAIL:   now-1 time returned valid\n" )
		t.Fail()
	} else {
		fmt.Fprintf( os.Stderr, "OK:     now-1 time returned invalid\n" )
	}
}

func Test_bw_equals( t *testing.T ) {
	h1 := "host1"
	h2 := "host2"
	h3 := "host3"
	p1 := "4360"
	p2 := ""
	v1 := "1"
	v2 := "2"
	v3 := "3"
	key := "cookie"
	id1 := "r1"


	failures := 0
	now := time.Now().Unix()

	fmt.Fprintf( os.Stderr, "\n----------- pledge equality tests --------------\n" )
	bp1, _ := Mk_bw_pledge( &h1, &h2, &p1, &p2, now+300, now+600, 10000, 10000, &id1, &key, 42, false )
	bp1.Set_vlan( &v1, &v2 )

	bp2, _ := Mk_bw_pledge( &h1, &h2, &p1, &p2, now+400, now+800, 10000, 10000, &id1, &key, 42, false )		// different times, but overlap (expect equal)
	bp2.Set_vlan( &v1, &v2 )

	bp3, _ := Mk_bw_pledge( &h1, &h2, &p1, &p2, now+800, now+1800, 10000, 10000, &id1, &key, 42, false )		// time window after p1 (expect not equal)
	bp3.Set_vlan( &v1, &v2 )

	bp4, _ := Mk_bw_pledge( &h3, &h2, &p1, &p2, now+300, now+600, 10000, 10000, &id1, &key, 42, false )		// different name (expect not equal)
	bp4.Set_vlan( &v1, &v2 )

	bp5, _ := Mk_bw_pledge( &h2, &h1, &p2, &p1, now+300, now+600, 10000, 10000, &id1, &key, 42, false )		// names, proto reversed (expect equal)
	bp5.Set_vlan( &v2, &v1 )

	bp6, _ := Mk_bw_pledge( &h2, &h1, &p2, &p1, now+300, now+600, 10000, 10000, &id1, &key, 42, false )		// names, proto reversed different vlans (expect not equal)
	bp6.Set_vlan( &v3, &v1 )


	gp2 := Pledge(bp2)			// convert to generic pledge for calls
	gp3 := Pledge(bp3)
	gp4 := Pledge(bp4)
	gp5 := Pledge(bp5)
	gp6 := Pledge(bp6)
	if !bp1.Equals( &gp2 ) {
		failures++
		fmt.Fprintf( os.Stderr, "FAIL:   bp1 reporeted not equal to bp2 (overlapping time)\n" )
	}

	if bp1.Equals( &gp3 ) {
		failures++
		fmt.Fprintf( os.Stderr, "FAIL:   bp1 reporeted equal to bp3 (non overlap time)\n" )
	}

	if bp1.Equals( &gp4 ) {
		failures++
		fmt.Fprintf( os.Stderr, "FAIL:   bp1 reporeted equal to bp4 (different name)\n" )
	}

	if !bp1.Equals( &gp5 ) {
		failures++
		fmt.Fprintf( os.Stderr, "FAIL:   bp1 reporeted not equal to bp5 (names reversed)\n" )
	}
	
	if bp1.Equals( &gp6 ) {
		failures++
		fmt.Fprintf( os.Stderr, "FAIL:   bp1 reporeted equal to bp6 (names reversed, vlan different)\n" )
	}
	
	if failures > 0 {
		t.Fail()
	} else {
		fmt.Fprintf( os.Stderr, "OK:     all bandwidth pledge equal tests passed\n" )
	}
	fmt.Fprintf( os.Stderr, "\n" )
}
