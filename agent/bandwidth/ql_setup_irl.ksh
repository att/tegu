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

# -----------------------------------------------------------------------------------------------------------------
#	Mnemonic:	ql_setup_irl.ksh
#	Abstract:	Sets up ingress rate limiting bridge veth connector and flow-mods.
#
#	Author:		E. Scott Daniels
#	Date:		22 September 2014
#
#	Mods:		07 Oct 2014 - bug fix #227 - Only puts in flow mods when bridge is created
#				15 Oct 2014 - corrected bug introduced with #227 that was causing the br-rl f-mod
#						not to be created if it was deleted after the bridge was setup.
#				27 Oct 2014 - bug fix #241
#				10 Nov 2014 - Added connect timeout to ssh calls
#				17 Nov 2014	- Added timeouts on ssh commands to prevent "stalls" as were observed in pdk1.
#				04 Dec 2014 - Ensured that all crit/warn messages have a constant target host component.
#				28 Jan 2015 - Prevent an ssh call if -h indicates local host.
#				21 Jan 2015 - Set up on bridges other than br-int and to assume that br-rl name is <bridge>-rl
#						where bridge is the bridge that it is attached to (e.g. br-int-rl)
#						This script now requires ksh; reset #! (don't know why it was ever set to bash).
#				02 Feb 2016 - Added support for a parallel exit patch pair to reduce overhead.
# -----------------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $rhost $argv0: $@" >&2
}

function ensure_ok
{
	if (( $1 > 100 ))			# timeout
	then
		shift
		logit "abort: ssh timout: $@  target-host: $rhost  [FAIL]"
		rm -f $tmp/PID$$.*
		exit 1
	fi

	if (( $1 != 0 ))
	then
		shift
		logit "abort:  $@  target-host: $rhost  [FAIL]"
		rm -f $tmp/PID$$.*
		exit 1
	fi
}

function warn_if_bad
{
	if (( $1 != 0 ))
	then
		shift
		logit "$@	[WARN]"
		warned=1
	fi
}

# create the rate limit bridge
function mk_bridge
{
	$forreal timeout 15  $sudo /usr/bin/ovs-vsctl add-br $1-rl
	ensure_ok $? "unable to make bridge $1-rl"
	logit "created bridge $1-rl"
}

# remove the bridge
function rm_bridge
{
	if [[ -z $1 ]]
	then
		logit "no bridge passed to rm_bridge  [WARN]"
		return
	fi

	logit "deleting bridge $1-rl"
	$forreal timeout 15  $sudo /usr/bin/ovs-vsctl del-br $1-rl
}

# create our veth that will be used for the loop round
function mk_veth
{
	$forreal timeout 15  $sudo ip link add  $ve0 type veth peer name $ve1
	ensure_ok $? "unable to create veth link $ve0-$ve1"

	$forreal timeout 15  $sudo ip link set dev $ve0  up
	ensure_ok $? "unable to bring up veth link end point $ve0"

	$forreal timeout 15  $sudo ip link set dev $ve1 up
	ensure_ok $? "unable to bring up veth link end point $ve1"
	logit "created veth pair $ve0-$ve1   [OK]"
}

# delete the veth link
function rm_veth
{
	logit "deleting link"
	$forreal timeout 15  $sudo ip link set dev $ve0 down
	$forreal timeout 15  $sudo ip link set dev $ve1 down
	$forreal timeout 15  $sudo ip link delete  $ve0 type veth peer name $ve1
}

# detach the ports -- ignore output and return codes
function detach_veth
{
	$forreal timeout 15  $sudo ovs-vsctl --if-exists del-port $ve0 >/dev/null 2>&1
	$forreal timeout 15  $sudo ovs-vsctl --if-exists del-port $ve1 >/dev/null 2>&1
}

# attach the veth to the bridge and its rate limit (-rl) counterpart.
function attach_veth
{
	if [[ -z $1 ]]
	then
		logit "no bridge passed to attach_veth  [WARN]"
		return
	fi

	typeset have0=0
	typeset have1=0
	ql_suss_ovsd | egrep "$ve0|$ve1"
	ql_suss_ovsd | awk -v target0=" $ve0 "  -v target1=" $ve1 " '
		/^port: / {
			if( match( $0, target0 ) ) {
				have0=1;
				next;
			}
			if( match( $0, target1 ) ) {
				have1=1;
				next;
			}
		}

		END {
			printf( "%d %d\n", have0, have1 );
		}
	' | read have0 have1

	if (( have0 && have1 ))
	then
		logit "veth pair $ve0 -> $ve1 already exists; no action [OK]"
		return
	fi

	logit "cleaning previous attachments: $ve0 and $ve1 to $1 and $1-rl   [OK]"
	# ensure that they are gone
	detach_veth $1

	$forreal timeout 15  $sudo ovs-vsctl add-port $1 $ve0 			#-- set interface $ve0  ofport=4000
	ensure_ok $? "unable to attach veth $ve0 to $1"

	$forreal timeout 15  $sudo ovs-vsctl add-port $1-rl  $ve1 		#-- set interface $ve1  ofport=4001
	ensure_ok $? "unable to attach veth $ve1 to $1-rl"

	logit "attached veth  $ve0 and $ve1 to $1 and $1-rl   [OK]"
}

# delete the patch pair p0 and p1 passed in as $1 and $2
function rm_patch
{
	typeset p0=$1
	typeset p1=$2

	$forreal $sudo  ovs-vsctl --if-exists del-port $p0 >/dev/null 2>&1			# these can fail if not there, and that's ok
	$forreal $sudo  ovs-vsctl --if-exists del-port $p1 >/dev/null 2>&1
}

# Attach the patch pair to the bridges setting the necessary no flood option on the 
# main bridge side so we don't try to write to the port. Parm $1 is the bridge
# name and the assumption that $1-rl is the ratelimiting bridge.
# Assumes $1 is patch0 (main bridge side) and $2 is patch1 (rl bridge side)
#
# Both ports must exist before they can be configured.
function attach_patch
{
	typeset bridge=$1
	typeset port=0
	typeset p0=$2
	typeset p1=$3
	typeset have0=0
	typeset have1=0

	if [[ -z $p1 ]]			# if p0 is nil, p1 will be too, so need test just the one
	then
		logit "warn: invalid patch names given to attach_patch: p0=$p0 p1=$p1"
		warned=1
		return
	fi

	ql_suss_ovsd | awk -v target0=" $p0 "  -v target1=" $p1 " '
		/^port: / {
			if( match( $0, target0 ) ) {
				have0=1;
				next;
			}
			if( match( $0, target1 ) ) {
				have1=1;
				next;
			}
		}

		END {
			printf( "%d %d\n", have0, have1 );
		}
	' | read have0 have1

	if (( have0 && have1 ))
	then
		logit "patch pair $p0 -> $p1 already exists; no action [OK]"
		return
	fi

	rm_patch $p0 $p1

	# the add port commands here always generate an error msg to stderr referencing the ovs
	# log, and this seems normal.  There is a _warning_ in the log indicating that the 
	# network device which matches the port name cannot be opened. This is expected as 
	# this is a patch between logical bridges and doesn't involve any real or OS 'hardware'.
	# to avoid panic, the stderr is captured and squelched unless the return is not good.
	if ! $forreal $sudo ovs-vsctl add-port $bridge-rl $p1 >/tmp/PID$$.std 2>&1		# port creation first
	then
		logit "unable to add patch port $p1"
		cat /tmp/PID$$.std 2>&1
		warned=1
		return
	fi

	if ! $forreal $sudo ovs-vsctl add-port $bridge $p0 >/tmp/PID$$.std 2>&1
	then
		logit "unable to create port $p0"
		cat /tmp/PID$$.std 2>&1
		$forreal $sudo ovs-vsctl --if-exists del-port $p1
		warned=1
		return
	fi

	$forreal $sudo ovs-vsctl set interface $p1 type=patch "options:peer=$p0"	# point ports at the other bridge's port
	if (( $? != 0 ))
	then
		logit "unable to set $p0 as peer for $p1"
		$forreal $sudo ovs-vsctl --if-exists del-port $p1
		$forreal $sudo ovs-vsctl --if-exists del-port $p0
		warned=1
		return
	fi

	if ! $forreal $sudo ovs-vsctl set interface $p0 type=patch "options:peer=$p1"
	then
		logit "unable to set type to patch $p0"
		$forreal $sudo ovs-vsctl --if-exists del-port $p1
		$forreal $sudo ovs-vsctl --if-exists del-port $p0
		warned=1
		return
	fi

	if ! $forreal $sudo ovs-ofctl mod-port $bridge $p0  noflood						# prevent writing to the patch, it's an exit only ramp from the rl bridge
	then
		logit "attempt to set noflood on $p0 failed  [WARN]"
	fi

	# verify that there are valid port numbers (they won't be valid until everything is set)
	if [[ -z $forreal ]]			# skip this check if in -n mode as there won't be anything :)
	then
		ql_suss_ovsd | awk -v target="$p1" ' $4 == target { print $3 }' | read port
		if (( port <= 0 ))
		then
			logit "port $p1 created, but port number isn't good: $port	[WARN]"
			$forreal $sudo ovs-vsctl --if-exists del-port $p1
			$forreal $sudo ovs-vsctl --if-exists del-port $p0
			warned=1
			return
		fi
	fi

	if [[ -z $forreal ]]			# skip this check if in -n mode as there won't be anything :)
	then
		ql_suss_ovsd | awk -v target="$p0" ' $4 == target { print $3 }' | read port
		if (( port <= 0 ))
		then
			logit "port $p0 created, but port number isn't good: $port	[WARN]"
			$forreal $sudo ovs-vsctl --if-exists del-port $p1
			$forreal $sudo ovs-vsctl --if-exists del-port $p0
			warned=1
			return
		fi
	fi


	logit "patch pair attached:  $p0 --> $p1"
}

function usage
{
	echo "$argv0 version 2.0/11216" >&2
	echo "usage: $argv0 [-D] [-h host] [-n] [-p link-prefix] [-s] [bridge(s)]" >&2
	echo "   -D causes all rate limiting bridges, ports and links to be removed"
	echo "	bridge is the bridge that the rate limit bridge should be attached to"
	echo "  if not supplied, all bridges listed in the agent config file are setup/deleted."
}

# -------------------------------------------------------------------------------------------
if (( $( id -u ) != 0 ))
then
	sudo="sudo"
fi
config="${TEGU_AGENT_CONFIG:-tegu_agent.cfg}"

tmp=${TMP:-/tmp}
argv0="${0##*/}"
ssh_opts="-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"

ssh=""									# -h is deprecated; these must run locally now.
rhost="$(hostname)"						# remote host name for messages
rhost_parm=""							# remote host parameter (-h xxx) for commands that need it

forreal=""
no_exe=""
traceon="set -x"
traceoff="set +x"
link_prefix="qosirl"
force_attach=0
delete=0
parallel=1								# -s sets off (single pipe) and does not create the patch pair for outbound traffic
irl_fmod_cookie=0xdead					# might need to come from config someday
expected_fmods=2						# number of fmods expected to live on rate limiting brige

typeset -C bandwidth											# must ensure this is set to handle missing config file
ql_parse_config -f $config >/tmp/PID$$.cfg && . /tmp/PID$$.cfg		# xlate config file into variables and source them

while [[ $1 == "-"* ]]
do
	case $1 in
		-D)	delete=1;;
		-h)	
			logit "abort: -h is deprecated. $0 must run on the local host not through ssh."
			exit 1
			;;

		-n)	
			no_exec="-n"
			forreal="echo noexec (-n) is set, would run: "
			traceon=""
			traceoff=""
			;;

		-p)	link_prefix="$2"; shift;;

		-s)	parallel=0;;						# single pipe mode

		-\?)	usage;
				exit;;
	esac

	shift
done

if [[ -n $1 ]]
then
	bridge_list="$@"							# assume bridges to operate supplied on the command line
else
	bridge_list="${bandwidth.rate_limit}"		# else operate on all rate limit bridges in the config
fi

for bridge in $bridge_list				# set up or delete all bridges in the list/on the command line
do
	warned=0

	ve0="${bridge}-${link_prefix}0"		# bloody namespace is flat, add bridge as a prefix
	ve1="${bridge}-${link_prefix}1"		# veth pair endpint names that go into OVS
	patch0="${bridge}-${link_prefix}10"
	patch1="${bridge}-${link_prefix}11"

	if (( delete ))
	then
		logit "deleting ingress rate limiting configuration (bridge, ports, veth pair) for $bridge   [OK]"
		detach_veth	$bridge					# bring the ports down and remove from the bridges
		rm_veth	$bridge						# delete the link
		if (( parallel ))
		then
			rm_patch $patch0 $patch1
		fi
		rm_bridge $bridge					# finally drop the bridge
	else
		if (( !parallel ))
		then
			expected_fmods=1				# only once bounce back fmod expected if not  a parallel patch installed
		fi

		if [[ -e /etc/tegu/no_irl ]]		# check here -- we allow delete to go even when no-irl is set
		then
			logit "abort: /etc/tegu/no_irl file exists which prevents setting up ingress rate limiting bridge and flow-mods	[WARN]"
			rm -f /tmp/PID$$.*
			exit 1
		fi

		act_bridge_list=/tmp/PID$$.brdgek		# list of active bridges -- don't need to create if there
		link_list=/tmp/PID$$.link

		timeout 15 $sudo ovs-vsctl -d bare list bridge|grep name >$act_bridge_list
		ensure_ok $? "unable to get active bridge list"

		add_fmod=0
		if ! grep -q $bridge-rl $act_bridge_list			# if requested bridge is  not active in OVS
		then
			logit "making rate limiting bridge: $bridge-rl	[OK]"
			mk_bridge $bridge
			force_attach=1
			add_fmod=1										# new bridge must have needed flow-mods too
		else
			timeout 15 $sudo ovs-ofctl dump-flows $bridge-rl |grep -c cookie=${irl_fmod_cookie} | read fmod_count
			if (( fmod_count != expected_fmods ))
			then
				add_fmod=1									# f-mod gone missing; force creation
			fi
		fi

		timeout 15 ip link > $link_list						# links will show the veth pair, but NOT the patch 
		warn_if_bad $? "unable to generate a list of links"

		#if ! grep -q "$link_prefix" $link_list				# no veth found, make it
		if ! grep -q "$ve0" $link_list						# no veth found, make it
		then
			mk_veth	$bridge
			attach_veth $bridge
		else
			c=0
			if (( ! force_attach ))							# don't need to search if force attachement set above
			then
				ql_suss_ovsd  >/tmp/PID$$.udata				# fix #241; ensure that veth are attached to bridges
				c=$( grep -c $link_prefix /tmp/PID$$.udata )
			fi
			if (( c != 2 ))				# didn't exist, or pair existed, but if we had to create *-rl then we must attach it
			then
				attach_veth	$bridge
			fi
		fi

		if (( parallel ))									# if creating a parallel patch pair, check for it and create if missing
		then
			if (( ! warned ))
			then
				if (( ! force_attach ))
				then
					if ! $sudo ovs-vsctl list interface $patch0 >/dev/null 2>&1
					then
						force_attach=1
					else
						if ! $sudo ovs-vsctl list interface $patch1 >/dev/null 2>&1
						then
							force_attach=1
						fi
					fi
				fi
				if ((force_attach ))				# need to add the patch
				then
					#rm_patch $patch0 $patch1			# ensure they are gone; if one existed we don't want to fail trying to create it again
					attach_patch $bridge $patch0 $patch1
				else
					logit "patch pair exists, not creating  [OK]"
				fi
			else
					logit "earlier warnings issued; patch pair not being created  [WARN]"
			fi
		fi

		if (( ! warned ))
		then
			$forreal timeout 15 $sudo ovs-ofctl mod-port $bridge $ve0 noflood		# must do before setting fmods
			warn_if_bad $? "unable to set no flood on $bridge:$ve0"
		fi

		if (( ! warned  &&  add_fmod ))						# finally add flow-mods that are needed to support this
		then
			send_ovs_fmod $no_exec -t 0 --match -a --action  -X add $irl_fmod_cookie $bridge-rl 		# arp traffic not allowed to pass
			if (( parallel )) 								# if parallel, we need 2: one to flip traffic to the patch and one to drop if received on patch
			then
				ql_suss_ovsd | awk -v target_in=$ve1 -v target_out=$patch1 '				# get the port numbers for each pipe
					$4 == target_in { in_port = $3; next; }
					$4 == target_out { out_port = $3; next; }
					END {
						printf( "%d %d\n", in_port, out_port )
					}
				' | read in_port out_port

				if [[ -n $forreal ]]
				then
					logit "no exec (-n) mode; setting dummy port numbers for flow-mod setting"
					in_port=91111
					out_port=90000
				fi

				if (( in_port <= 0 || out_port <= 0 ))
				then
					logit "abort: unable to find both input and output port numbers for flow-mods for $ve1 and/or $patch1"
				else
					send_ovs_fmod $no_exec -t 0 --match -i $in_port --action  -o $out_port add $irl_fmod_cookie $bridge-rl 		# default f-mod for *-rl that bounces packets out from whence they came
					warn_if_bad $? "unable to set flip flow-mod on $bridge_rl inport=$in_port outport=$out_port"

					
					send_ovs_fmod $no_exec -t 0 --match -i $out_port --action  -X add $irl_fmod_cookie $bridge-rl 				# drop packets received on the patch; patch is an exit ramp
					warn_if_bad $? "unable to set drop flowmod on rate limiting bridge $bridge-rl"
				fi
			else											# not parallel, just need a bounce back fmod
				# bug fix #227 -- only replace the flow mod when bridge is created, or if we cannot find it
				send_ovs_fmod $rhost_parm $no_exec -t 0 --match --action -b add $irl_fmod_cookie $bridge-rl 		# default f-mod for *-rl that bounces packets out from whence they came
				warn_if_bad $? "unable to set flow-mod on $bridge-rl"
			fi
		fi

	fi
done

rm -f $tmp/PID$$.*
exit 0


