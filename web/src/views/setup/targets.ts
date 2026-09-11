// What it takes to get each kind of client through the proxy.
//
// Two separate problems per target, and they are kept separate on the
// page because they fail differently: ROUTING decides whether the
// request reaches ihttp at all, and TRUST decides whether the client
// accepts the certificate ihttp mints for the host. An empty log is the
// first. A TLS error is the second.
//
// Every snippet is filled in from GET /api/info, so what is shown is
// this instance's real proxy address and real certificate path rather
// than a plausible-looking example.

export interface Facts {
  proxyURL: string
  consoleAddr: string
  caPath: string
  probeURL: string
}

export interface Snippet {
  label: string
  lang: string
  code: string
}

export interface Target {
  id: string
  name: string
  // One sentence on what this client does and does not do on its own.
  summary: string
  routing: (f: Facts) => Snippet[]
  // Left out when trust needs nothing beyond `ihttp cert install`.
  trust?: (f: Facts) => string
  // What no snippet can fix, when there is such a thing.
  caveat?: string
  // How to make this client fetch the probe, so the setup can be
  // checked rather than assumed.
  probe: (f: Facts, url: string) => Snippet
}

const envBlock = (f: Facts) => `export HTTP_PROXY=${f.proxyURL}
export HTTPS_PROXY=${f.proxyURL}
export http_proxy=${f.proxyURL}
export https_proxy=${f.proxyURL}
export NO_PROXY=${f.consoleAddr}`

export const TARGETS: Target[] = [
  {
    id: 'curl',
    name: 'curl',
    summary:
      'Reads the lower-case proxy variables for plain HTTP and both cases for HTTPS. The --cacert form needs no trust store at all, which makes it the best first test of whether the proxy itself works.',
    routing: (f) => [
      { label: 'One request', lang: 'sh', code: `curl -x ${f.proxyURL} https://example.com/` },
      {
        label: 'Without touching any trust store',
        lang: 'sh',
        code: `curl -x ${f.proxyURL} --cacert ${f.caPath} https://example.com/`,
      },
    ],
    probe: (f, url) => ({ label: 'Run this', lang: 'sh', code: `curl -x ${f.proxyURL} ${url}` }),
  },
  {
    id: 'shell',
    name: 'Any shell',
    summary:
      'Sets the environment for this shell and everything started from it. The command only prints - the shell is what applies it, so you can read it first and nothing outside that shell changes.',
    routing: (f) => [
      { label: 'sh, bash, zsh', lang: 'sh', code: `eval "$(ihttp env)"` },
      { label: 'fish', lang: 'sh', code: 'ihttp env --shell fish | source' },
      { label: 'PowerShell', lang: 'sh', code: 'ihttp env --shell powershell | iex' },
      { label: 'Undo it', lang: 'sh', code: 'ihttp env --unset' },
      { label: 'Or by hand', lang: 'sh', code: envBlock(f) },
    ],
    trust: () =>
      `ihttp cert install\n\n# ihttp env also sets NODE_EXTRA_CA_CERTS and the JVM proxy\n# properties. It leaves SSL_CERT_FILE, REQUESTS_CA_BUNDLE and\n# their kind alone, because those REPLACE a runtime's whole\n# trust store - pass --replace-ca-bundle if that is what you want.`,
    probe: (f, url) => ({
      label: 'Run this in the prepared shell',
      lang: 'sh',
      code: `curl ${url}`,
    }),
  },
  {
    id: 'node',
    name: 'Node.js',
    summary:
      'Node core has never read the proxy variables on its own. Node 24 added NODE_USE_ENV_PROXY=1, which ihttp env sets. Earlier versions need an explicit agent.',
    routing: (f) => [
      { label: 'Node 24 and later', lang: 'sh', code: `eval "$(ihttp env)"\nnode app.js` },
      {
        label: 'Earlier, with undici',
        lang: 'js',
        code: `import { ProxyAgent, setGlobalDispatcher } from 'undici'

setGlobalDispatcher(new ProxyAgent('${f.proxyURL}'))`,
      },
    ],
    trust: (f) => `export NODE_EXTRA_CA_CERTS=${f.caPath}`,
    probe: (f, url) => {
      const [host, port] = hostPort(f.proxyURL)
      const probeHost = hostOf(url)

      return {
        label: 'Run this',
        lang: 'sh',
        code: `# Node 24 and later, through the environment:
NODE_USE_ENV_PROXY=1 HTTP_PROXY=${f.proxyURL} node -e "fetch('${url}').then(r=>r.text()).then(console.log)"

# Any version, pointing at the proxy by hand:
node -e "require('http').get({host:'${host}',port:${port},path:'${url}',headers:{Host:'${probeHost}'}},r=>r.pipe(process.stdout))"`,
      }
    },
  },
  {
    id: 'python',
    name: 'Python',
    summary:
      'urllib and requests follow the proxy variables. They verify against their own bundle, so HTTPS needs the certificate too - which is the step people miss.',
    routing: () => [{ label: 'Prepare the shell', lang: 'sh', code: `eval "$(ihttp env)"` }],
    trust: (f) =>
      `# The blunt way, which discards Python's other anchors:\nexport REQUESTS_CA_BUNDLE=${f.caPath}\nexport SSL_CERT_FILE=${f.caPath}\n\n# ihttp env --replace-ca-bundle does exactly that.\n# The tidier way is adding the certificate to certifi's bundle:\ncat ${f.caPath} >> "$(python3 -m certifi)"`,
    probe: (f, url) => ({
      label: 'Run this',
      lang: 'sh',
      code: `HTTP_PROXY=${f.proxyURL} python3 -c "import urllib.request; print(urllib.request.urlopen('${url}').read().decode())"`,
    }),
  },
  {
    id: 'go',
    name: 'Go',
    summary:
      'http.DefaultTransport reads the proxy variables, and Go reads the port in NO_PROXY correctly. Trust comes from the system store, so ihttp cert install is enough.',
    routing: () => [
      { label: 'Prepare the shell', lang: 'sh', code: `eval "$(ihttp env)"\ngo run .` },
    ],
    probe: (f, url) => ({
      label: 'Run this',
      lang: 'sh',
      code: `cat > /tmp/ihttp-probe.go <<'EOF'
package main

import (
	"fmt"
	"io"
	"net/http"
)

func main() {
	res, err := http.Get("${url}")
	if err != nil {
		panic(err)
	}

	defer res.Body.Close()

	b, _ := io.ReadAll(res.Body)
	fmt.Println(string(b))
}
EOF

HTTP_PROXY=${f.proxyURL} go run /tmp/ihttp-probe.go`,
    }),
  },
  {
    id: 'java',
    name: 'Java and the JVM',
    summary:
      'A JVM reads properties rather than the environment, which is what JAVA_TOOL_OPTIONS is for, and it keeps its own trust store.',
    routing: (f) => {
      const [host, port] = hostPort(f.proxyURL)

      return [
        { label: 'Prepare the shell', lang: 'sh', code: `eval "$(ihttp env)"` },
        {
          label: 'Or by hand',
          lang: 'sh',
          code: `export JAVA_TOOL_OPTIONS="-Dhttp.proxyHost=${host} -Dhttp.proxyPort=${port} -Dhttps.proxyHost=${host} -Dhttps.proxyPort=${port}"`,
        },
      ]
    },
    trust: () => 'ihttp cert install --java',
    caveat:
      'An IDE may override the JVM properties with its own proxy setting. If traffic from a run configuration does not appear, look there before looking here.',
    probe: (f, url) => ({
      label: 'Run this',
      lang: 'sh',
      code: `curl -x ${f.proxyURL} ${url}   # then the same from your JVM`,
    }),
  },
  {
    id: 'docker',
    name: 'Docker',
    summary:
      'A container does not share the host loopback, so the proxy address inside it is not the one shown above. The certificate has to be mounted in.',
    routing: (f) => {
      const [, port] = hostPort(f.proxyURL)

      return [
        {
          label: 'An env file and a mounted certificate',
          lang: 'sh',
          code: `ihttp env --shell env > .env
sed -i'' -e 's|//localhost:|//host.docker.internal:|g; s|//127.0.0.1:|//host.docker.internal:|g' .env

docker run --rm \\
  --env-file .env \\
  --add-host host.docker.internal:host-gateway \\
  -v ${f.caPath}:/usr/local/share/ca-certificates/ihttp.crt:ro \\
  your-image`,
        },
        {
          label: 'On Linux, the other way round',
          lang: 'sh',
          code: `docker run --rm --network host \\
  -e HTTP_PROXY=http://127.0.0.1:${port} \\
  -e HTTPS_PROXY=http://127.0.0.1:${port} \\
  -v ${f.caPath}:/usr/local/share/ca-certificates/ihttp.crt:ro \\
  your-image`,
        },
      ]
    },
    trust: () =>
      'The mount puts the certificate in place. Most images still need\n# update-ca-certificates run once, in the image or on start.',
    probe: (f, url) => {
      const [, port] = hostPort(f.proxyURL)

      return {
        label: 'Run this',
        lang: 'sh',
        code: `docker run --rm --add-host host.docker.internal:host-gateway curlimages/curl \\
  -x http://host.docker.internal:${port} ${url}`,
      }
    },
  },
  {
    id: 'browser',
    name: 'A browser',
    summary:
      'A throwaway profile, already pointed at the proxy, with trust handled and the browser own chatter switched off - so the log holds what you did rather than what the browser did.',
    routing: () => [
      { label: 'Launch one', lang: 'sh', code: 'ihttp browser chrome' },
      { label: 'Or Firefox', lang: 'sh', code: 'ihttp browser firefox' },
      { label: 'Or from the start', lang: 'sh', code: 'ihttp serve --chrome' },
    ],
    trust: () =>
      'Handled: Chromium is launched ignoring certificate errors, and\n# the certificate is imported into the Firefox profile.\n# For your own everyday browser instead: ihttp cert install',
    probe: (f, url) => ({
      label: 'Open this in the launched browser',
      lang: 'text',
      code: url,
    }),
  },
  {
    id: 'client',
    name: 'Postman, Insomnia and the like',
    summary:
      'An API client has a proxy setting of its own that overrides the environment. There is nothing to prepare in a shell.',
    routing: (f) => [
      {
        label: 'In the app settings',
        lang: 'text',
        code: `Proxy: ${f.proxyURL}
Certificate verification: off, or trust the CA in the system store`,
      },
    ],
    trust: () => 'ihttp cert install',
    probe: (f, url) => ({ label: 'Send this from the app', lang: 'text', code: url }),
  },
]

// hostOf is the host a URL names, for a client that has to be handed the
// proxy and the target host separately.
function hostOf(raw: string): string {
  try {
    return new URL(raw).host
  } catch {
    return raw
  }
}

// hostPort splits a proxy URL for the runtimes that want the two apart.
function hostPort(proxyURL: string): [string, string] {
  try {
    const u = new URL(proxyURL)

    return [u.hostname || 'localhost', u.port || '80']
  } catch {
    return ['localhost', '6080']
  }
}
