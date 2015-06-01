
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
	p1 := new_pw( now + 3600, now + 7200 )
	p2 := new_pw( now + 3, now + 600 )			// completely before p1 (expect false)
	p3 := new_pw( now + 8000, now + 9800 )		// completely after p1 (expect false)
	p4 := new_pw( now + 600, now + 4800 )		// commence overlap		(expect true)
	p5 := new_pw( now + 800, now + 3800 )		// expiry overlap		(expect true)
	p6 := new_pw( now + 3 , now + 9800 )		// completely engulf	(expect true)
	p7 := new_pw( now + 3 , now + 3600 )		// before ending where p1 starts (expect false)

	fmt.Fprintf( os.Stderr, "------- pledge window tests ---------\n" );
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
