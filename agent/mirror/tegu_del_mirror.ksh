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

#
#                            AT&T - PROPRIETARY
#              THIS FILE CONTAINS PROPRIETARY INFORMATION OF
#            AT&T AND IS NOT TO BE DISCLOSED OR USED EXCEPT IN
#                  ACCORDANCE WITH APPLICABLE AGREEMENTS.
#
#                         Copyright (c) 2015 AT&T
#                   Unpublished and Not for Publication
#                          All Rights Reserved
#
#       Name:      tegu_del_mirror
#       Usage:     tegu_del_mirror [-v] <name>
#       Abstract:  This script deletes a mirror, named by <name>, from openvswitch.
#
#       Author:    Robert Eby
#       Date:      4 February 2015
#
#       Mods:      4 Feb 2015 - created
#                  11 Feb 2015 - remove temp file
#					25 Jun 2015 - Corrected PATH.
#

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

PATH=$PATH:/sbin:/usr/bin:/bin 		# must pick up agent augmented path
echo=:
if [ "$1" == "-v" ]
then
	shift
	echo=echo
fi

if [ $# != 1 ]
then
	echo "usage: tegu_del_mirror [-v] name" >&2
	exit 1
fi
if [ ! -x /usr/bin/ovs-vsctl ]
then
	echo "tegu_del_mirror: ovs-vsctl is not installed or not executable." >&2
	exit 2
fi

bridgename=br-int		# bridge will always be br-int for now
sudo=sudo
[ "`id -u`" == 0 ] && sudo=

mirrorname=$1

$echo $sudo ovs-vsctl list mirror "$mirrorname"
$sudo ovs-vsctl list mirror "$mirrorname" > /tmp/m$$ && {
	# get output_port UUID
	uuid=`grep output_port /tmp/m$$ | sed 's/.*: //'`
	# get name from uuid
	$echo $sudo ovs-vsctl list port $uuid
	pname=`$sudo ovs-vsctl list port $uuid | grep name | tr -d '" ' | cut -d: -f2`
	# if it is a GRE port, with the right name, remove port
	case "$pname" in
	gre-$mirrorname)
		$echo $sudo ovs-vsctl del-port $bridgename $pname
		$sudo ovs-vsctl del-port $bridgename $pname
		;;
	esac

	# get mirror UUID
	uuid=`grep _uuid /tmp/m$$ | sed 's/.*: //'`
	$echo $sudo ovs-vsctl remove bridge $bridgename mirrors $uuid
	$sudo ovs-vsctl remove bridge $bridgename mirrors $uuid
	rm -f /tmp/m$$
	
	echo Mirror $mirrorname removed.
	exit 0
}

echo "tegu_del_mirror: mirror $mirrorname does not exist." >&2
rm -f /tmp/m$$
exit 3
