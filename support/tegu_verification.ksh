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
	$trace
	if grep -q "status.*ERROR" $1
	then
		return 1
	fi

	return 0
}


# returns failure if error not found in the output file
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
		echo "FAIL: $2"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1 
		fi
		return
	fi

	echo "OK:   $2"
}

# return true if the output failed (failure expected)
function validate_fail
{
	if isok $1
	then
		echo "FAIL: $2"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1 
		fi
		return
	fi

	echo "OK:   $2"
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
			echo "OK:   $3"
		else
			echo "FAIL: $fmsg, but did not contain expected data: $3"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1 
			fi
			return 
		fi
	else
		echo "FAIL: $3"
		(( errors++ ))
		if (( short_circuit ))
		then
			exit 1 
		fi
		return
	fi
}

# ensure that the command retured ok, and that the output does NOT contain
# the indicated string/pattern
# parms:  file "pattern" "message"
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
			echo "OK:   $3"
		else
			echo "FAIL: $fmsg, but contained unexpected data: ($2) $3"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1 
			fi
			return 
		fi
	else
		echo "FAIL: $3"
		(( errors++ ))
		return
	fi
}

# suss out the reservation id (rid) from the input file
function suss_rid
{
	awk '$1 == "id" { print $NF; exit( 0 ) }' $1
}

function capture
{
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
		-p)	proj_list="$2"; shift;;
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

vma=${1:-esd_ss1,esd_ss2}			# project 1
p1vm_list=( ${vma//,/ } )

vma=${2:-esd_ss1,esd_ss2}			# project 2 (by default these are the same project, so same vms)
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

# = = = = = = = = =  tests below here = = = = = = = = = = 

# ----- simple connectivity and response test (ping)
run tegu_req $secure $host ping >$single_file
capture  $single_file "ping test"
validate_ok $single_file "ping"

# ----- list host and network topology graph tests
run tegu_req $secure $host listhosts >$single_file
capture  $single_file "list hosts"
validate_ok $single_file "listhosts"

run tegu_req $secure $host graph >$single_file
capture  $single_file "network graph"
validate_ok $single_file "graph"


# ----- create a single project reservation and verify
run run tegu_req -T $secure $host reserve 10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "create reservation"
validate_ok $single_file "reserve single project"

suss_rid $single_file | read rid

#------ reservation verification and deletion
if [[ -n $rid ]]				# if we found the req id, list reservations and ensure it's listed, then delete
then
	run tegu_req $secure $host listres >$single_file
	capture $single_file "list reservation"
	validate_contains $single_file "id.*$rid" "list reservation"

	run tegu_req $secure $host cancel $rid not-my-cookie >$single_file		# should fail
	capture $single_file	"cancel with bad cookie"
	validate_fail $single_file "prevents reservation cancel with invalid cookie"

	run tegu_req $secure $host cancel $rid cookie	>$single_file			# this should be successful
	capture $single_file	"cancel with good cookie"
	validate_ok $single_file "cancel reservation"

else
	echo "WARN: no reservation ID; list and cancel functions not tested"
fi

# ----- verify reservation expires 
if (( skip_long == 0 ))
then
	run tegu_req -T $secure $host reserve 10M +30 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
	capture $single_file "reservation for expiration test"
	validate_ok $single_file "resere single project"
	suss_rid $single_file | read rid
	if [[ -n $rid ]]
	then
		echo "INFO: waiting 40s for reservation to expire"
		sleep 40
		run tegu_req $secure $host listres >$single_file
		capture $single_file "list reservations after expiration should have happened"
		validate_missing $single_file "id.*$rid" "reservation natural expiration"
	else
		echo "WRN: no reservation id found trying to verify reservation natural expiration"
	fi
else
	echo "INFO: skipped long running expiration test"
fi


# ----- verify reservation rejected for bad token and project
run tegu_req $secure $host reserve 10M +30 bad-token/$project1/${p1vm_list[0]},bad-token/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "reservation attempt with bad token"
validate_fail $single_file "reservation with bad token was rejected"
suss_rid $single_file | read rid

run tegu_req -T $secure $host reserve 10M +30 %t/bad_project/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "reservation attempt with bad project"
validate_fail $single_file "reservation with bad project was rejected"


# ----- verify user link caps can be set
run tegu_req $secure $host setulcap $project1 1 >$single_file
capture $single_file "set user link cap to 1%"
validate_contains $single_file "comment = user link cap set for.*1[%]*$" "set user link cap to 1%"

# ----- verify link cap is enforced (assuming 10G links, with 1% cap, a request for 500M should fail)
#trace="set -x"
run tegu_req -T $secure $host reserve 500M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "reservation attempt in excess of user link cap (expect fail)"
validate_contains -f $single_file "unable to generate a path" "rejected reservation attempt for more than user link cap (1%)"
trace="set +x"

run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "user allowed to reserve less than link cap setting"
validate_ok $single_file "allowed to reserve less than 1% link cap"

# ---- set zero link cap
run tegu_req $secure $host setulcap $project1 0 >$single_file
capture $single_file "set user link cap to 0"
validate_contains $single_file "comment = user link cap set for" "set user link cap to 0"

# ---- ensure reservation request rejected when limit is 0
run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
capture $single_file "test reservation when link cap set to 0"
validate_fail $single_file "rejected reservation when user link cap set to 0"

# ---- return link cap to sane value
run tegu_req $secure $host setulcap $project1 90 >$single_file
capture $single_file "set user link cap back up to 90%"
validate_ok $single_file "reset user link cap to 90% for further testing"


# ------ VM to  external reservation
run tegu_req -T $secure $host reserve 5M +30 %t/$project1/${p1vm_list[0]},!//135.207.01.01 cookie >$single_file
capture $single_file "VM to external IP reservation test"
validate_ok $single_file "VM to external IP reservation test"

# ------ multi-project "half" reservations
# NOTE: if this fails, ensure that the VM in both projects have floating IP addresses attached. 
#trace="set -x"
run tegu_req -T $secure $host reserve 5M +30 %t/$project1/${p1vm_list[0]},!/$project2/${p2vm_list[0]} cookie >$single_file
capture $single_file "multi-project reservation test"
validate_ok $single_file "multi-project reservation (half)"
trace="set +x"

suss_rid $single_file | read rid

if [[ -n $rid ]]
then
	run tegu_req $secure $host listres >$single_file
	capture $single_file "list mulit-project reservation"
	validate_contains $single_file "id.*$rid" "list reservation after mulit-project reservation created"
fi



# ----- multiple concurrent reservations
if (( ${#p1vm_list[*]} < 4 ))
then
	echo "INFO: skipping concurrent reservation test; requires 4 VM names for project 1 on command line"
else
	run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie >$single_file
	capture $single_file "first reservation in multiple reservation"
	validate_ok $single_file "first reservation in concurrent reservation"
	suss_rid $single_file | read rida

	if [[ -n $rida ]]
	then
		run tegu_req -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[2]},%t/$project1/${p1vm_list[3]} cookie >$single_file
		capture $single_file "second reservation in multiple reservation"
		validate_ok $single_file "second reservation in concurrent reservation"
		suss_rid $single_file | read ridb
		
		if [[ -n $ridb ]]
		then
			run tegu_req $secure $host listres >$single_file
			capture $single_file "concurrent reservations list reservation"
			validate_contains $single_file "id.*$rida" "concurrent list reservation has first reservation"
			validate_contains $single_file "id.*$ridb" "concurrent list reservation has second reservation"
		
			run tegu_req $secure $host cancel $rida cookie >$single_file		# should fail
			capture $single_file	"cancel first of the concurrent reservations"
			validate_ok $single_file "cancel first of concurrent reservations"

			echo "INFO: waiting 20s before checking reservation state (allow cancelled reservation to clear)"
			sleep 20

			run tegu_req $secure $host listres >$single_file
			capture $single_file "list reservation after cancelling the first"
			validate_missing $single_file "id.*$rida" "cancelled concurrent resrvation is not listed"
			validate_contains $single_file "id.*$ridb" "second concurrent reservation continues to be listed after first cancelled"
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
