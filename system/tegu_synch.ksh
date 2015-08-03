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

#!/usr/bin/env ksh

#
#	Mnemonic:	tegu_synch
#	Abstract:	Simple script to take a snapshot of the checkpoint environment and push it off to
#				the stand-by hosts.  Stand-by hosts are expected to be listed one per line in
#				If the first parameter on the command line is "recover" then this script will
#				attempt to restore the most receent synch file into the chkpt directory.
#
#				CAUTION:  A _huge_ assumption is made here -- the TEGU_ROOT directory on each
#					host is the same!
#
#   Exit:		an exit code of 1 is an error while an exit code of 2 is a warning and the calling
#				script might be able to ignore it depending on what action was attempted. An exit
#				code of zero is good.
#
#	Date:		25 July 2014
#	Author:		E. Scott Daniels
#
#	Mod:		14 Jan - added provision to reload that will search for an old style name (no host)
#					if one with a host cannot be found.
#				02 Jul 2015 - correct bug that allowed an old gzip file to be used when newer
#					checkpoint files exist.
# --------------------------------------------------------------------------------------------------

trap "rm -f /tmp/PID$$.*" 1 2 3 15 EXIT

function verify_id
{
	whoiam=$( id -n -u )
	if [[ $whoiam != $tegu_user ]]
	then
		echo "Only the tegu user ($tegu_user) can affect the state; ($(whoami) is not acceptable)     [FAIL]"
		echo "'sudo su $tegu_user' and rerun this script"
		echo ""
		exit 1
	fi
}

# check for standby mode and bail  if this is a standby node
function ensure_active
{

	if [[ -f $standby_file ]]
	then
		echo "WRN: this host is a tegu standby host and does not synch its files"
		exit 0
	fi
}

# capture a config file
function cap_config
{
	if [[ -f $etcd/$1 ]]
	then
		if ! cp $etcd/$1 chkpt/
		then
			echo "WRN: unable to capture a copy of the config file ($etcd/$1) with the check point files" >&2
		fi
	else
		echo "WRN: $etcd/$1  does not exist, config was captured with the checkpoint files" >&2
	fi
}

# restore a configuration file
function restore_config
{
	if [[ -f $1 ]]				# if a config file was captured with the checkpoint files
	then
		if  cp $1 $etcd/
		then
			echo "config file ($1) restored and copied into $etcd    [OK]" >&2
		else
			echo "WRN: unable to copy config file ($1) into $etcd" >&2
		fi
	fi
}

# --------------------------------------------------------------------------------------------------

export TEGU_ROOT=${TEGU_ROOT:-/var}
logd=${TEGU_LOGD:-/var/log/tegu}
libd=${TEGU_LIBD:-/var/lib/tegu}
etcd=${TEGU_ETCD:-/etc/tegu}
chkptd=$TEGU_LIBD/chkpt
tegu_user=${TEGU_USER:-tegu}

ssh_opts="-o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"

standby_file=$etcd/standby
restore=0

case $1 in
	restore)	#restore the latest sync into the chkpt directory
		restore=1
		;;

	*)	ensure_active;;
esac

if [[ ! -d $etcd ]]
then
	echo "WRN: tegu seems not to be installed on this host: $etcd doesn't exist" >&2
	exit 1
fi

verify_id			# ensure we're running with tegu user id

if ! cd $libd
then
	echo "CRI: unable to switch to tegu lib directory: $libd   [FAIL]" >&2
	exit 1
fi

if [[ ! -d chkpt ]]
then
	if (( restore ))
	then
		if ! mkdir chkpt
		then
			echo "CRI: unable to create the checkpoint directory $PWD/chkpt" >&2
			exit 1
		fi
	else
		echo "WRN: no checkpoint directory exists on this host, nothing done" >&2
		exit 2
	fi
fi


if (( ! restore ))				# take a snap shot of our current set of chkpt files and the current config from $etcd
then
	if [[ ! -f $etcd/standby_list ]]
	then
		echo "WRN: no stand-by list ($etcd/standby_list), nothing done" >&2
		exit 2
	fi
	
	# chef gets pissy if we restore things into etc, so we don't any more.
	#cap_config tegu.cfg					# we need to snarf several of the current config files too
	#ls $etcd/*.json | while read jfile
	#do
	#	cap_config ${jfile##*/}
	#done

	m=$( date +%M )						# current minutes
	n=$(( (m/5) * 5 ))					# round current minutes to previous 5 min boundary
	host=$( hostname )
	tfile=/tmp/PID$$.chkpt.tgz			# local tar file
	rfile=$libd/chkpt_synch.$host.$n.tgz	# remote archive (we should save just 12 so no need for cleanup)
	tar -cf - chkpt |gzip >$tfile
	
	while read host
	do
		if ! scp $ssh_opts -o PasswordAuthentication=no $tfile $tegu_user@$host:$rfile
		then
			echo "CRI: unable to copy the synch file to remote host $host" >&2
		else
			echo "successful copy of sync file to $host   [OK]"
		fi
	done <$etcd/standby_list
else
	ls -t $libd/chkpt_synch.*.*.tgz | head -1 |read synch_file
	if [[ -z $synch_file ]]
	then
		ls -t $libd/chkpt_synch.*.tgz | head -1 | read synch_file		# old style (no host name)
		if [[ -z $synch_file ]]
		then
			echo "WRN: cannot find a synch file, no restore of synchronised data" >&2
			exit 2
		fi
	fi

	bfile=$libd/synch_backup.tgz		# we'll take a snapshot of what was there just to prevent some accidents
	tar -cf - chkpt | gzip >$bfile

	newer_list=$( find $chkptd -name "resmgr_*" -newer $synch_file )
	if [[ -n $newer_list ]]
	then
		echo "WRN: did not restore from tar, $synch_file is older than some checkpoint files"
	else
		gzip -dc $synch_file | tar -xf - 		# unload the synch file into the directory
		echo "synch file ($synch_file) was restored into $PWD/chkpt    [OK]"

		# chef gets pissy if we do this, so we don't any more.
		#restore_config chkpt/tegu.cfg					# restore the config files
		#ls chkpt/*.json | while read jfile
		#do
		#	restore_config $jfile
		#done
	fi
fi

exit 0
