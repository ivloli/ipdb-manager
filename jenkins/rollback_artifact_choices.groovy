if (ACTION != 'Rollback') {
    return []
}

def cmd = '''
curl -s -u 'dnps:SXKvkwRICe743VPhTkKIbA==' \
  'https://artifactory.gainetics.io/artifactory/observe-dnps/prod-ipdb-xdb-manager/' \
| grep -oE 'href="ipdb-manager-offline-linux-amd64-[^"]+\\.tar\\.gz"' \
| sed -e 's/^href="//' -e 's/"$//' \
| awk '!seen[$0]++' \
| head -n 10
'''

def proc = ['bash', '-lc', cmd].execute()
proc.waitFor()

if (proc.exitValue() != 0) {
    return ['ERROR: failed to fetch artifacts']
}

def lines = proc.in.text.readLines().findAll { it?.trim() }
return lines ?: ['NO_ARTIFACT_FOUND']
