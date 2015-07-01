#!/usr/bin/env ksh

#	Mnemonic:	tegu_verification.ksh
#	Abstract:	Runs a few regression(ish) tests to verify that tegu is working.
#				Must have OS_PASSWORD and OS_AUTH_URL defined in the environment.
#				project and vm names are supplied on the command line as:
#					project1:project2 user vm1,vm2...vmN vma,vmb,...vmn
#				The first set of VMs are assumed to be for project 1 and the
#				second set for project 2.
#
#				We assume this is being run from the host where tegu is running.
#
# 	Date: 		31 July 2014
#	Author:		E. Scott Daniels
#---------------------------------------------------------------------------------



# check the file for a simple ERROR or not. Return good (0)
# if error not observed.
function isok
{
	if grep -q "status.*ERROR" $1		# explicitly found an error
	then
		return 1
	fi

	if ! grep -q "status.*OK" $1		# must also have an OK in the mix
	then								# if tegu not running it won't have ERROR
		return 1
	fi

	return 0
}


# checks for error in the output and returns good if found.  This can be used
# to ensure something did fail, so don't look for an OK.
function isnotok
{
	$trace
	if grep -q "status.*ERROR" $1
	then
		return 0
	fi

	return 1
}

# check to see if command was success (success expected)
function validate_ok
{
	if ! isok $1
	then
		echo "FAIL: $global_comment"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1
		fi
		return
	fi

	echo "OK:   $global_comment"
}

# return true if the output failed (failure expected)
function validate_fail
{
	if isok $1
	then
		echo "FAIL: $global_comment"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1
		fi
		return
	fi

	echo "OK:   $global_comment"
}

# ensure that the command retured ok, and that the output contains the
# indicated string/pattern
# parms:  file "pattern" "message"
function validate_contains
{
	$trace
	typeset not=""
	typeset fmsg="executed ok"
	typeset test="isok"
	if [[ $1 == "-f" ]]		# expect failure and failure output should contain
	then
		shift
		test="isnotok"
		fmsg="failed as expected"
	fi

	if $test $1
	then
		if grep -q "$2" $1
		then
			echo "OK:   $global_comment"
		else
			echo "FAIL: $fmsg, but did not contain expected data: $global_comment"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1
			fi
			return
		fi
	else
		echo "FAIL: $global_comment"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1
		fi
		return
	fi
}

# ensure that the command retured ok (or failed if -f set), and that the output does NOT contain
# the indicated string/pattern.
# parms:  file "pattern"
function validate_missing
{
	typeset not=""
	typeset fmsg="executed ok"
	typeset test="isok"
	if [[ $1 == "-f" ]]		# expect failure and failure output should contain
	then
		shift
		test="isnotok"
		fmsg="failed as expected"
	fi

	if $test $1
	then
		if ! grep -q "$2" $1
		then
			echo "OK:   $global_comment"
		else
			echo "FAIL: $fmsg, but contained unexpected data: ($2) $global_comment"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1
			fi
			return
		fi
	else
		echo "FAIL: $global_comment"
		(( errors++ ))
		return
	fi
}

# suss out the reservation id (rid) from the input file
function suss_rid
{
	awk '$1 == "id" { print $NF; exit( 0 ) }' $1
}

# copy contents of $1 to outfile giving a separation line with $2 as a comment
# sets global_comment to $2 for other functions to use
function capture
{
	global_comment="$2"
	if [[ -n $out_file ]]
	then
		echo "===== $2 =====" >>$out_file
		echo "$last_cmd" >>$out_file
		cat $1 >>$out_file
		echo "" >>$out_file
	fi
}

function usage
{
	echo "usage: $argv0 [-o output-capture-file] [-h host[:port]] [-s] [-S] [-p project1:project2] [-u user] p1vm1,p1vm2...,p1vmN p2vm1,p2vm2...,p2vmN"
	echo "-s skips long running tests"
	echo "-S turns off secure (ssl) mode"
	echo "p1vm1 (etc) are the VM names for project 1 and project 2"
}

function run
{
	last_cmd="$@"
	"$@"
}

# --------------------------------------------------------------------------------
argv0=$0
secure=""
out_file=""
single_file=/tmp/PID$$.out
host="-h 127.0.0.1:29444"
errors=0
skip_long=0
short_circuit=0						# -e enables this; stop on first error
project_list=""						# cloudqos:cloudqos used if not supplied with -p

export OS_USERNAME=${OS_USERNAME:-tegu}
user=$OS_USERNAME					# -u overrides

while [[ $1 == -* ]]
do
	case $1 in
		-e) short_circuit=1;;
		-h)	host="-h $2"; shift;;
		-o)	out_file=$2; shift;;
		-p)	project_list="$2"; shift;;
		-s)	skip_long=1;;			# short tests only
		-S)	secure="-s";;
		-u) user=$2; shift;;
		-\?)	usage
				exit 0
				;;

		-*)	usage
			exit 1
			;;
	esac

	shift
done

export OS_USERNAME=$user						# ensure this is out there with -u change if needed
project1=${project_list:-cloudqos:cloudqos}
project2=${project1##*:}
project1=${project1%%:*}

vma=${1:-esd_ss1}			# project 1
p1vm_list=( ${vma//,/ } )

vma=${2:-esd_ss2}			# project 2
p2vm_list=( ${vma//,/ } )


if [[ -z $OS_PASSWORD ]]
then
	echo "OS_PASSWORD must be defined in the environment"
	exit 1
fi

if [[ -z OS_AUTH_URL ]]
then
	echo "OS_AUTH_URL must be defined in the environment"
	exit 1
fi

if [[ -n $out_file ]]		# truncate existing file
then
	>$out_file
fi

# = = = = = = = = = = = = = = = = = =  tests below here = = = = = = = = = = = = = = = = = = =

# ----- simple connectivity and response test (ping) ---------------------------------
run tegu_req $secure $host ping >$single_file
capture  $single_file "ping test"
validate_ok $single_file
if grep -q POST $single_file
then
	echo "ABORT:  tegu seems to not be running"
	exit 1
fi

# ----- list host and network topology graph tests ---------------------------------
run tegu_req -k project="$project1" $secure $host listhosts >$single_file
capture  $single_file "list hosts"
validate_ok $single_file
count=0
for v in $p1vm_list
do
	if ! grep -q $v $single_file
	then
		echo "FAIL: $v not found in list host output for project $project1"
		(( count++ ))
	fi
done

if (( count ))
then
	cat $single_file
	echo "unable to continue with missing VM"
	exit 1
else
	echo "OK:   found all project1 VMs in the listhost output"
fi


run tegu_req $secure $host graph >$single_file
capture  $single_file "network graph"
validate_ok $single_file


# ----- create a single project reservation and verify ---------------------------------
run tegu_req -T $secure $host reserve 10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reserve single project"
validate_ok $single_file

suss_rid $single_file | read rid


#------ reservation verification, dup rejected, and deletion ---------------------------------
if [[ -n $rid ]]				# if we found the req id, list reservations and ensure it's listed, then delete
then
	run tegu_req $secure $host listres >$single_file
	capture $single_file "list reservation"
	validate_contains $single_file "id.*$rid"

	# attempt a duplicate; should fail
	run tegu_req -T $secure $host reserve 10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "ensure dup reservations are rejected"
	validate_fail $single_file

	run tegu_req $secure $host cancel $rid not-my-cookie >$single_file		# should fail
	capture $single_file	"prevents reservation cancel with invalid cookie"
	validate_fail $single_file

	run tegu_req $secure $host cancel $rid cookie	>$single_file			# this should be successful
	capture $single_file	"cancel reservation"
	validate_ok $single_file
	echo "INFO: waiting 20s for reservation cancel to take effect"
	sleep 20

else
	echo "WARN: no reservation ID; list and cancel functions not tested"
fi

# ----- reservation with 0 bandwidth is rejected --------------------------------------------
run tegu_req -T $secure $host reserve 0 +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 0 bandwidth"
validate_fail $single_file

run tegu_req -T $secure $host reserve 10M,0 +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 10M,0 bandwidth"
validate_fail $single_file

run tegu_req -T $secure $host reserve 0,10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 0,10M bandwidth"
validate_fail $single_file


# ----- verify reservation expires  ---------------------------------
if (( skip_long == 0 ))
then
	run tegu_req -T $secure $host reserve 10M +30 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "reservation for expiration test"
	validate_ok $single_file "resere single project"
	suss_rid $single_file | read rid
	if [[ -n $rid ]]
	then
		echo "INFO: waiting 40s for reservation to expire"
		sleep 40
		run tegu_req $secure $host listres >$single_file
		capture $single_file "reservation natural expiration"
		validate_missing $single_file "id.*$rid"
	else
		echo "WRN: no reservation id found trying to verify reservation natural expiration"
	fi
else
	echo "INFO: skipped long running expiration test"
fi


# ----- verify reservation rejected for bad token and project ---------------------------------
run tegu_req $secure $host reserve 10M +30 bad-token/$project1/${p1vm_list[0]},bad-token/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation with bad token is rejected"
validate_fail $single_file
suss_rid $single_file | read rid

run tegu_req -T $secure $host reserve 10M +30 %t/bad_project/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation with bad project is rejsected"
validate_fail $single_file


# ----- verify user link caps can be set ------------------------------------
run tegu_req $secure $host setulcap $project1 1 >$single_file
capture $single_file "can set user link cap (1%)"
validate_contains $single_file 'comment = user link cap set for.*1[%]*$'

# ----- verify link cap is enforced (assuming 10G links, with 1% cap, a request for 500M should fail) ---------------------
#trace="set -x"
run tegu_req -T $secure $host reserve 500M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation rejected when exceeds user cap"
validate_contains -f $single_file "unable to generate a path"
trace="set +x"

run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation accepted when less than link cap"
validate_ok $single_file

# ---- set zero link cap ----------------------------------------------------
run tegu_req $secure $host setulcap $project1 0 >$single_file
capture $single_file "set user link cap to 0"
validate_contains $single_file "comment = user link cap set for"

# ---- ensure reservation request rejected when limit is 0 ----------------------------------------------------
run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation rejected when link cap is 0"
validate_fail $single_file

# ---- return link cap to sane value ----------------------------------------------------
run tegu_req $secure $host setulcap $project1 90 >$single_file
capture $single_file "user link cap can be set back up (90%)"
validate_ok $single_file


# ------ oneway reservation testing ------------------------------------------------
run tegu_req -T $secure $host owreserve 10M +90 %t//${p1vm_list[0]},!//135.207.43.60 owcookie voice >$single_file
capture $single_file "oneway reservation can be created"
validate_ok $single_file

suss_rid $single_file | read rid
if [[ -n $rid ]]
then
	run tegu_req cancel all owcookie >$single_file
	capture $single_file "oneway reservation can be cancelled"
	validate_ok $single_file
	echo "INFO: pausing 20s to let cancel fall off"
	sleep 20
	run tegu_req listres | grep -c $rid |read count
	if (( count ))
	then
		echo "FAIL: still found oneway reservation in list after cancel"
	else
		echo "OK:   oneway reservation cancel successfully removed the resrvation from the list"
	fi
else
	echo "SKIP: skipping oneway cancel test: no reservation id found"
fi

# ------ multi-project "half" reservations
# NOTE: if this fails, ensure that the VM in both projects have floating IP addresses attached.
#trace="set -x"
run tegu_req -T $secure $host reserve 5M +30 %t/$project1/${p1vm_list[0]},!/$project2/${p2vm_list[0]} cookie voice >$single_file
capture $single_file "can make half of a multi project reservation"
validate_ok $single_file
trace="set +x"

suss_rid $single_file | read rid

if [[ -n $rid ]]
then
	run tegu_req $secure $host listres >$single_file
	capture $single_file "list reservation following a multi project reservation"
	validate_contains $single_file
fi


# ----- cancel all reservations with a given cookie ----------------------------------------------------
run tegu_req listres >$single_file
capture $single_file "reservation state before cancel tests"

run tegu_req cancel all invalid-cookie >$single_file
run tegu_req listres >$single_file.more
cat $single_file.more >>$single_file
capture $single_file "canceling all with non-matching cookie leave non-matching reservations"
grep -c time $single_file.more | read count
if (( count <= 0 ))
then
	echo "FAIL:	cancel all reservations with cookie leave non-matching reservations"
else
	echo "OK:   cancel all left non-matching reservations"
fi
rm -f $single_file.more

run tegu_req cancel all cookie >$single_file
capture $single_file "cancel all matching cookie"
tegu_req listres >$single_file
grep  "time =" $single_file| grep -c -v "time.*=.*15" | read count
if (( count > 0 ))
then
	echo "FAIL:	reservations with cookie remain"
	capture $single_file "cookie reservations remained"
	grep  "time =" $single_file| grep  -v "time.*=.*15"
else
	echo "OK:   delete of all reservations with the same cookie"
fi

run tegu_req listres >$single_file
capture $single_file "reservation state after cancel tests"


# ----- multiple concurrent reservations ----------------------------------------------------
if (( ${#p1vm_list[*]} < 4 ))
then
	echo "INFO: skipping concurrent reservation test; requires 4 VM names for project 1 on command line"
else
	echo "INFO: pausing 20s to allow previous reservations to expire"
	sleep 20
	run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "create first reservation in multiple reservation test"
	validate_ok $single_file
	suss_rid $single_file | read rida

	if [[ -n $rida ]]
	then
		run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[2]},%t/$project1/${p1vm_list[3]} cookie voice >$single_file
		capture $single_file "able to make second reservation in multiple reservation test"
		validate_ok $single_file
		suss_rid $single_file | read ridb
		
		if [[ -n $ridb ]]
		then
			run tegu_req $secure $host listres >$single_file
			capture $single_file "list concurrent reservations"

			global_comment="concurrent list reservation has first reservation"
			validate_contains $single_file "id.*$rida" 
			global_comment="concurrent list reservation has second reservation"
			validate_contains $single_file "id.*$ridb" 
		
			run tegu_req $secure $host cancel $rida cookie >$single_file		# should fail
			capture $single_file	"cancel first of the concurrent reservations"
			validate_ok $single_file

			echo "INFO: waiting 20s before checking reservation state <allow cancelled reservation to clear>"
			sleep 20

			run tegu_req $secure $host listres >$single_file
			capture $single_file "list contains second reservation after first in multiple reservation test cancelled"
			validate_missing $single_file "id.*$rida" 
			global_comment="second concurrent reservation continues to be listed after first cancelled"
			validate_contains $single_file "id.*$ridb" 
		else
			echo "WARN: could not find reservation id for second reservation; rest of concurrent reservation tests skipped"
		fi
	else
		echo "WARN: could not find reservation id for first reservation; rest of concurrent reservation tests skipped"
	fi
fi





# --------------------------------------------------------------------------------------------
echo "$errors errors discovered"

rm -f /tmp/PID$$.*
