#!/usr/bin/env ksh

#	Mnemonic:	tegu_verification.ksh
#	Abstract:	Runs a few regression(ish) tests to verify that tegu is working.
#				Must have OS_PASSWORD and OS_AUTH_URL defined in the environment.
#				project and vm names are supplied on the command line as:
#					project1 user vm1,vm2...vmN 
#
#				We assume this is being run from the host where tegu is running.
#				Specify -h host:port on command line if tegu is running on another
#				host.
#
#				This script does _not_ verify flow-mods are set with reservations.
#				There is another script which will do that level of testing. This
#				script is concerned with the operation and interface of tegu's API.
#			
#				Real-time diagnostics are written to stdout (info/ok/fail messages)
#				while the verbose data (output captured and analysed) is written to 
#				a file in /tmp.
#
#				Sample invocation:
#					tegu_verification.ksh $1 -o /tmp/verify.out -p cloudqos:SDS  -u tegu daniels1,daniels2,daniels4,daniels8 sds4
#
# 	Date: 		31 July 2014
#	Author:		E. Scott Daniels
#	Mods:		18 Nov 2016 - Updated to support endpoints.
#---------------------------------------------------------------------------------


function log_failure
{
	echo "$1"
	echo "$1" >>$out_file
}

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
		log_failure "FAIL: $global_comment"
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
		log_failure "FAIL: $global_comment"
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
			log_failure "FAIL: $fmsg, but did not contain expected data: ($2)"
			echo "	log eye catcher: $global_comment"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1
			fi
			return
		fi
	else
		log_failure "FAIL: $global_comment"
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
			log_failure "FAIL: $fmsg, but contained unexpected data: ($2) $global_comment"
			(( errors++ ))
			if (( short_circuit ))
			then
				exit 1
			fi
			return
		fi
	else
		log_failure "FAIL: $global_comment"
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
	echo "usage: $argv0 [-e] [-E external-ip] [-o output-capture-file] [-h host[:port]] [-O] [-s] [-S] [-p project] [-u user] vm1,vm2...,vmN"
	echo "-e causes a quick exit on first failure, otherwise all tests are attempted"
	echo "-O  old mode (pre 4.x versions of tegu)"
	echo "-s skips long running tests"
	echo "-S turns off secure (ssl) mode"
	echo "vm1 (etc) are the VM names that this uses to set reservations up. Supply 2 or 4 different names"
	echo "if 4 names are supplied, then concurrent reservation tests can be executed."
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
host="-T -h 127.0.0.1:29444"
errors=0
skip_long=0
short_circuit=0						# -e enables this; stop on first error
project_list=""						# cloudqos:cloudqos used if not supplied with -p
external_ip=135.207.223.80			# bornoeo, but it shouldn't matter
disable_endpts=0

export OS_USERNAME=${OS_USERNAME:-tegu}
user=$OS_USERNAME					# -u overrides

while [[ $1 == -* ]]
do
	case $1 in
		-e) short_circuit=1;;
		-E)	external_ip=$2; shift;;
		-h)	host="-T -h $2"; shift;;
		-o)	out_file=$2; shift;;
		-O)	disable_endpts=1;;
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

vma=${1:-esd_ss1}			# project 1
p1vm_list=( ${vma//,/ } )

vmb=${2:-esd_ss2}			# project 2
p2vm_list=( ${vmb//,/ } )

typeset -A endpts

if (( disable_endpts ))
then
	for vm in ${vma//,/ } 
	do
		endpts[$vm]="$vm"
	done
else
	OS_TENANT_NAME=$project1 tegu_osdig -a -v epid ${vma//,/ } | while read vm tuple
	do
		vm=${vm%:}
		tuple=${tuple##* }						# if vm has multiple interfaces only pick the last one
		if [[ tuple == "missing" ]]
		then
			echo "cannot convert vmname into endpoint uuid: $vm"
			exit 1
		fi

		endpts[$vm]="${tuple%%,*}"
	done
fi

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
run tegu_req -c $secure $host ping >$single_file
capture  $single_file "ping test"
validate_ok $single_file
if grep -q POST $single_file
then
	echo "ABORT:  tegu seems to not be running"
	exit 1
fi

# ----- list host and network topology graph tests ---------------------------------
if (( disable_endpts ))
then
	# must force project finding in old tegu
	run tegu_req -c -k project="$project1" $secure $host listhosts >$single_file
else
	run tegu_req -c  $secure $host listhosts >$single_file
fi
capture  $single_file "list hosts"
validate_ok $single_file
count=0
for v in $p1vm_list
do
	if ! grep -q "${endpts[$v]}" $single_file
	then
		log_failure "FAIL: $v (${endpts[$v]}) not found in list host output for project $project1"
		(( count++ ))
	fi
done

if (( count ))
then
	#cat $single_file
	echo "unable to continue with missing VM(s)"
	exit 1
else
	echo "OK:   found all project1 VMs in the listhost output"
fi

run tegu_req -c $secure $host graph >$single_file
capture  $single_file "network graph"
validate_ok $single_file


# ----- create a single project reservation and verify ---------------------------------
run tegu_req -c -T $secure $host reserve 10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reserve single project"
validate_ok $single_file

suss_rid $single_file | read rid


#------ reservation verification, dup rejected, and deletion ---------------------------------
if [[ -n $rid ]]				# if we found the req id, list reservations and ensure it's listed, then delete
then
	run tegu_req -c $secure $host listres >$single_file
	capture $single_file "list reservation"
	validate_contains $single_file "id.*$rid"

	# attempt a duplicate; should fail
	run tegu_req -c -T $secure $host reserve 10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "ensure dup reservations are rejected"
	validate_fail $single_file

	run tegu_req -c $secure $host cancel $rid not-my-cookie >$single_file		# should fail
	capture $single_file	"prevents reservation cancel with invalid cookie"
	validate_fail $single_file

	run tegu_req -c $secure $host cancel $rid cookie	>$single_file			# this should be successful
	capture $single_file	"cancel reservation"
	validate_ok $single_file
	echo "INFO: waiting 20s for reservation cancel to take effect"
	sleep 20

else
	echo "WARN: no reservation ID; list and cancel functions not tested"
fi

# ----- reservation with 0 bandwidth is rejected --------------------------------------------
run tegu_req -c -T $secure $host reserve 0 +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 0 bandwidth"
validate_fail $single_file

run tegu_req -c -T $secure $host reserve 10M,0 +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 10M,0 bandwidth"
validate_fail $single_file

run tegu_req -c -T $secure $host reserve 0,10M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reject reservation with 0,10M bandwidth"
validate_fail $single_file


# ----- verify reservation expires  ---------------------------------
if (( skip_long == 0 ))
then
	run tegu_req -c -T $secure $host reserve 10M +30 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "reservation for expiration test"
	validate_ok $single_file "resere single project"
	suss_rid $single_file | read rid
	if [[ -n $rid ]]
	then
		echo "INFO: waiting 40s for reservation to expire"
		sleep 40
		run tegu_req -c $secure $host listres >$single_file
		capture $single_file "reservation natural expiration"
		validate_missing $single_file "id.*$rid"
	else
		echo "WRN: no reservation id found trying to verify reservation natural expiration"
	fi
else
	echo "INFO: skipped long running expiration test"
fi


# ----- verify reservation rejected for bad token and project ---------------------------------
run tegu_req -c $secure $host reserve 10M +30 bad-token/$project1/${p1vm_list[0]},bad-token/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation with bad token is rejected"
validate_fail $single_file
suss_rid $single_file | read rid

run tegu_req -c -T $secure $host reserve 10M +30 %t/bad_project/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation with bad project is rejsected"
validate_fail $single_file


#====== LINK CAPACITY TESTING ==================================================================
# ----- verify user link caps can be set ------------------------------------
run tegu_req -c $secure $host setulcap $project1 1 >$single_file
capture $single_file "can set user link cap (1%)"
validate_contains $single_file 'comment = user link cap set for.*1[%]*$'

# ----- verify link cap is enforced (assuming 10G links, with 1% cap, a request for 500M should fail) ---------------------
#trace="set -x"
run tegu_req -c -T $secure $host reserve 500M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation rejected when exceeds user cap (failure in relaxed mode is OK)"
validate_contains -f $single_file "unable to generate a path"
trace="set +x"

run tegu_req -c -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation accepted when less than link cap (failure in relaxed mode is OK)"
validate_ok $single_file

# ---- set zero link cap ----------------------------------------------------
run tegu_req -c $secure $host setulcap $project1 0 >$single_file
capture $single_file "set user link cap to 0"
validate_contains $single_file "comment = user link cap set for"

# ---- ensure reservation request rejected when limit is 0 ----------------------------------------------------
run tegu_req -c -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
capture $single_file "reservation rejected when link cap is 0"
validate_fail $single_file

# ---- return link cap to sane value ----------------------------------------------------
run tegu_req -c $secure $host setulcap $project1 90 >$single_file
capture $single_file "user link cap can be set back up (90%)"
validate_ok $single_file


# ====== ONEWAY RESERVATION TESTING =====================================================================================
run tegu_req -c -T $secure $host owreserve 10M +90 %t//${p1vm_list[0]},!//135.207.43.60 owcookie voice >$single_file
capture $single_file "oneway reservation can be created"
validate_ok $single_file

suss_rid $single_file | read rid
if [[ -n $rid ]]
then
	run tegu_req -c $secure $host cancel all owcookie >$single_file
	capture $single_file "oneway reservation can be cancelled"
	validate_ok $single_file
	echo "INFO: pausing 20s to let cancel fall off"
	sleep 20
	run tegu_req -c $secure $host listres | grep -c $rid |read count
	if (( count ))
	then
		log_failure "FAIL: still found oneway reservation in list after cancel"
	else
		echo "OK:   oneway reservation cancel successfully removed the resrvation from the list"
	fi
else
	echo "SKIP: skipping oneway cancel test: no reservation id found"
fi


# ======== PASSTHRU TESTING =========================================================================================
run tegu_req -c -T $secure $host passthru +90 %t/%p/${p1vm_list[0]} ptcookie >$single_file
capture $single_file "passthru reservation can be created (failure if relaxed=false is expected)"
validate_ok $single_file

suss_rid $single_file | read rid
if [[ -n $rid ]]
then
	run tegu_req -c $secure $host cancel all ptcookie >$single_file
	capture $single_file "passthru reservation can be cancelled"
	validate_ok $single_file
	echo "INFO: pausing 20s to let cancel fall off"
	run tegu_req -c $secure $host listres >$single_file
	sleep 20
	run tegu_req -c $secure $host listres | grep -c $rid |read count
	if (( count ))
	then
		log_failure "FAIL: still found passthru reservation in list after cancel"
		capture $single_file "reservation list active when passthru check failed (res=$rid)"
	else
		echo "OK:   passthru reservation cancel successfully removed the resrvation from the list"
	fi
else
	echo "SKIP: skipping passthur cancel test: no reservation id found"
fi

# ---- set the ulcap to 0 to see that passthru is rejected -------------------------------
run tegu_req -c $secure $host setulcap $project1 0 >$single_file
capture $single_file "set ulcap to 0 to ensure it blocks passthru"
validate_contains $single_file 'comment = user link cap set for.*0[%]*$'

run tegu_req -c -T $secure $host passthru +90 %t/%p/${p1vm_list[0]} ptcookie >$single_file
capture $single_file "passthru reservation is rejected if ulcap is 0"
validate_fail $single_file


# ---- return link cap to sane value ----------------------------------------------------
run tegu_req -c $secure $host setulcap $project1 90 >$single_file
capture $single_file "reset ulcap back up after passthru reject test (cap=90%)"
validate_ok $single_file



# ====== MULTI-PROJECT "HALF" RESERVATIONS ===============================================================================
#trace="set -x"
run tegu_req -c -T $secure $host reserve 5M +30 %t/$project1/${p1vm_list[0]},!//${external_ip} cookie voice >$single_file
capture $single_file "can make half of a multi project reservation"
validate_ok $single_file
trace="set +x"

suss_rid $single_file | read rid

if [[ -n $rid ]]
then
	run tegu_req -c $secure $host listres >$single_file
	capture $single_file "list reservation following a multi project reservation"
	validate_contains $single_file
fi


# ----- cancel all reservations with a given cookie ----------------------------------------------------
run tegu_req -c $host $secure listres >$single_file
capture $single_file "reservation state before cancel tests"

run tegu_req -c $host $secure cancel all invalid-cookie >$single_file
run tegu_req -c $host $secure listres >$single_file.more
cat $single_file.more >>$single_file
capture $single_file "canceling all with non-matching cookie leave non-matching reservations"
grep -c time $single_file.more | read count
if (( count <= 0 ))
then
	log_failure "FAIL:	cancel all reservations with cookie leave non-matching reservations"
else
	echo "OK:   cancel all left non-matching reservations"
fi
rm -f $single_file.more

run tegu_req -c $host $secure cancel all cookie >$single_file
capture $single_file "cancel all matching cookie"
tegu_req -c $host $secure listres >$single_file
grep  "time =" $single_file| awk '($NF)+0 > 15 { count++ } END { print count+0 }' | read count
if (( count > 0 ))
then
	log_failure "FAIL:	reservations with cookie remain"
	echo "NOTE:  this can happen if reservations that _should_ have been deleted earlier still exist" >>$out_file
	capture $single_file "$count cookie reservations remained with time >15s  (reservation dump below)"
	grep  "time =" $single_file| grep  -v "time.*=.*15"
else
	echo "OK:   delete of all reservations with the same cookie"
fi

run tegu_req -c $host $secure listres >$single_file
capture $single_file "reservation state after cancel tests"


# ----- multiple concurrent reservations ----------------------------------------------------
if (( ${#p1vm_list[*]} < 4 ))
then
	echo "INFO: skipping concurrent reservation test; requires 4 VM names for project 1 on command line"
else
	echo "INFO: pausing 20s to allow previous reservations to expire"
	sleep 20
	run tegu_req -c -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[0]},%t/$project1/${p1vm_list[1]} cookie voice >$single_file
	capture $single_file "create first reservation in multiple reservation test"
	validate_ok $single_file
	suss_rid $single_file | read rida

	if [[ -n $rida ]]
	then
		run tegu_req -c -T $secure $host reserve 5M +90 %t/$project1/${p1vm_list[2]},%t/$project1/${p1vm_list[3]} cookie voice >$single_file
		capture $single_file "able to make second reservation in multiple reservation test"
		validate_ok $single_file
		suss_rid $single_file | read ridb
		
		if [[ -n $ridb ]]
		then
			run tegu_req -c $secure $host listres >$single_file
			capture $single_file "list concurrent reservations"

			global_comment="concurrent list reservation has first reservation"
			validate_contains $single_file "id.*$rida" 
			global_comment="concurrent list reservation has second reservation"
			validate_contains $single_file "id.*$ridb" 
		
			run tegu_req -c $secure $host cancel $rida cookie >$single_file		# should fail
			capture $single_file	"cancel first of the concurrent reservations"
			validate_ok $single_file

			echo "INFO: waiting 20s before checking reservation state <allow cancelled reservation to clear>"
			sleep 20

			run tegu_req -c $secure $host listres >$single_file
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
