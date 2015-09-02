#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ---------------------------------------------------------------------------
#

#	Mnemonic:	ql_setup_ipt
#	Abstract:	Delete and then install the iptables rules in mangle that do the right thing for our DSCP marked traffic
#				This must also handle all of the routers that are created in namespaces, so we first generate a
#				set of commands for the main iptables, then generate the same set for each nameespace. This script is 
#				broken from a larger script which (before ssh_broker) used ssh to execute the commands generated. The
#				structure of generating a single command file, and then executing has been maintained, but the script
#				doesn't support executing the commands on a remote host.  As a result of breaking this out, the script
#				does support a delete option (-D) which will delete all of the iptables rules in the mangle table that
#				we have added. 
#
#				We have discovered that setting up iptabes across a large (+2000) set of name spaces can take a noticable
#				amount of time, and there are indications that this setup might impact current flows.  This observation
#				has been made by seeing increased numbers of retransmissions from some applications during the execution,
#				but has not directly been tied to these activities.  To this end, the script will only add the iptables
#				rules if it appears that the rules are not missing.  We assume that listing the iptables in each name
#				space, while taking time, will not have any impact on exising flows.
#
#	Author:		E. Scott Daniels
#	Date: 		28 Aug 2015	(yanked from setup_ovs_intermed)
#
#	Mods:
# -----------------------------------------------------------------------------------------------------------------------
#

# dump the current state of the mangle table and return good (0) if we find the expected
# set of classify rules. $1 is the command (sudo, or sudo ip netns <name>) allowing us to
# chceck either the system or a network namespace.  At the moment we only validate the 
# known possible values: x2e, x1a, x12 and the default 00; we should be smart enough to 
# only validate what was asked for by tegu based on the tegu config. 
#
function verify_current
{
	if (( force ))			# when force is on we always indicate that reload is needed
	then
		return 1
	fi

	cmd="$1"
	$cmd iptables -L -t mangle |awk -v dscp_list="${diffserv//,/ }"  '
		/DSCP.*match.*0x00.*CLASSIFY/ && ! have_00 { have_00++; have++; next; }
		/DSCP.*match.*0x2e.*CLASSIFY/ && ! have_2e { have_2e++; have++; next; }
		/DSCP.*match.*0x1a.*CLASSIFY/ && ! have_12 { have_1a++; have++; next; }
		/DSCP.*match.*0x12.*CLASSIFY/ && ! have_12 { have_12++; have++; next; }

		END {
			if( have == 4 )
				exit( 0 )
			exit( 1 )
		}
	'
	# put NOTHING in between awk and return!
	return $?
}

function usage
{
	echo "usage: $argv0 [-f] [-n] [-v] DSCP-values"
	echo "  -f : force, update everything regardless of perceived state"
	echo "  -n : no execute, just say what would be done"
	echo "  -v : verbose, may be a bit more chatty during operation"
	echo " DSCP values is a list of comma separated, decimal values (e.g. 184,104,72)"
	echo "     which have been shifted left (x4) from the values that are commonly"
	echo "     refferred to as a DSCP value. For example, DSCP value of 46 would be "
	echo "     passed in as 184."
	
}

# -------------------------------------------------------------------------------------------------------------------------
thost=$(hostname)							# this (target) host
argv0="$0"
force=0
verbose=0
delete=0						# -D sets causing rules to be deleted and not added
cmd_string=""					# normal space iptables command list
nscmd_string=""					# namespace command
cmd_file=/tmp/PID$$.cmds		# cmds to send to the remote to set ip stuff
nslist="/tmp/PID$$.nslist"		# list of name spaces from the remote host
err_file="/tmp/PID$$.ipterr"

while [[ $1 == "-"* ]]
do
	case $1 in 
		-D)	delete=1;;
		-f)	force=1;;
		-n)	no_exec_str="would execute: ";;
		-v) verbose=1;;

		-\?)	usage
				exit 0
				;;

		*)	echo "unrecognised option: $1" >&2
			usage
			exit 1
			;;	
	esac

	shift
done

if [[ -z $1 ]]
then
	echo "missing DSCP value(s) from command line" >&2
	usage >&2
	exit 1
fi

diffserv="$@"					# these are the values to set

sudo ip netns list >$nslist 2>$err_file
if (( $? != 0 ))
then
	echo "CRI: unable to get network name space list from target-host: ${thost#* }  [FAIL] [QOSSOM007]"
	sed 's/^/setup_iptables:/' $err_file >&2
fi

iptables_del_base="sudo iptables -D POSTROUTING -t mangle -m dscp --dscp"	# various pieces of the command string
iptables_add_base="sudo iptables -A POSTROUTING -t mangle -m dscp --dscp"
iptables_tail="-j CLASSIFY --set-class"

iptables_nsbase="sudo ip netns exec" 										# must insert name space name between base and mid
iptables_del_mid="iptables -D POSTROUTING -t mangle -m dscp --dscp"			# reset for the name space specific command
iptables_add_mid="iptables -A POSTROUTING -t mangle -m dscp --dscp"

echo "ecount=0" >$cmd_file										# we'll count errors and exit the command set with an error if > 0
(																
	need2run=0
																	# create the commands to send; first the master iptables rules, then rules for each name space
	if (( delete )) || ! verify_current	"sudo"						# need to set on the system table here
	then
		(( need2run++ ))
		echo "echo ====  master ==== >&2"							# add separators to the output when it's run
		if ((  delete  ))
		then
			echo "$iptables_del_base 0 $iptables_tail 1:2;"
			for d in ${diffserv//,/ }													# d will be 4x the value that iptables needs
			do
				echo "$iptables_del_base $((d/4)) $iptables_tail 1:6;"					# add in delete commands (just in case, but should not need)
			done
		else
			echo "$iptables_add_base 0 $iptables_tail 1:2 || (( ecount++ ))"
			for d in ${diffserv//,/ }
			do
				echo "$iptables_add_base $((d/4)) $iptables_tail 1:6 || (( ecount++ ))"		# we care about errors only on add commands
			done
		fi
	else
		if (( verbose ))
		then
			echo "iptables on the system is valid, no action needed  [OK]" >&2
		fi
	fi

	while read ns 																		# for each name space we found
	do
		if (( delete )) || ! verify_current "$iptables_nsbase $ns"						# verify this namespace
		then
			(( need2run++ ))

			if (( delete ))
			then
				echo "echo ==== $ns delete  ==== >&2"											# add separators to the output when it's run
				echo "$iptables_nsbase $ns $iptables_del_mid 0 $iptables_tail 1:2;" 			# odd ball delete case first
				for d in ${diffserv//,/ }
				do
					echo "$iptables_nsbase $ns $iptables_del_mid $((d/4)) $iptables_tail 1:6;"	# put in delete commands, one per dscp type
				done
			else
				echo "echo ==== $ns add  ==== >&2"							# add separators to the output when it's run
				echo "$iptables_nsbase $ns $iptables_add_mid 0 $iptables_tail 1:2 || (( ecount++ ))" 	# odd ball add case
				for d in ${diffserv//,/ }
				do
					echo "$iptables_nsbase $ns $iptables_add_mid $((d/4)) $iptables_tail 1:6 || (( ecount++ ))"		# one per dscp type
				done
			fi
		else
			if (( verbose ))
			then
				echo "iptables for namespace $ns is valid, no action needed  [OK]" >&2
			fi
		fi
	done <$nslist

	if (( need2run ))		# exit good if commands are in the file
	then
		exit 0
	fi

	exit 1					# no commands generated, signal parent that cmdfile can be trashed
) >>$cmd_file
if (( $? != 0 ))
then
	>$cmd_file
fi

rc=0
if [[ -s $cmd_file ]]								# something to execute
then
	echo 'exit $(( ecount > 0 ))' >>$cmd_file		# cause the command set to finish bad if there was an error (single quotes are REQUIRED!)

	if [[ -z $no_exec_str ]]								# empty string means we're live
	then
		ksh $cmd_file >$err_file 2>&1
		if (( $? != 0 ))
		then
			echo "CRI: unable to set iptables on target-host: ${thost#* }  [FAIL] [QOSSOM006]"
			echo "setup_iptables: output from command execution:" >&2
			sed 's/^/setup_iptables:/' $err_file >&2		# prefix so easier to id in agent log
			rc=1
		else
			echo "iptables successfully set up for mangle rules on target-host: ${thost#* }"
			if (( verbose ))
			then
				echo "output from command execution:" >&2
				cat $err_file >&2
			fi
		fi
	else
		sed "s/^/iptables setup: $no_exec_str /" $cmd_file >&2			# just say what we might do
	fi
else
	echo "$no_exec_str no iptables commands were needed, no action taken at all [OK]" >&2
fi

rm -f /tmp/PID$$.*

exit $rc
