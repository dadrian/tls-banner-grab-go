package ztls

import (
	"crypto/rsa"
	"encoding/binary"
	"encoding/json"
	"math/big"

	"github.com/zmap/zgrab/ztools/x509"
)

type TLSVersion uint16
type CipherSuite uint16

type ServerHello struct {
	Version             TLSVersion  `json:"version"`
	Random              []byte      `json:"random"`
	SessionID           []byte      `json:"session_id"`
	CipherSuite         CipherSuite `json:"cipher_suite"`
	CompressionMethod   uint8       `json:"compression_method"`
	OcspStapling        bool        `json:"ocsp_stapling"`
	TicketSupported     bool        `json:"ticket"`
	SecureRenegotiation bool        `json:"secure_renegotiation"`
	HeartbeatSupported  bool        `json:"heartbeat"`
}

// ServerCertificates represents a TLS certificates message in a format friendly to the golang JSON library.
// ValidationError should be non-nil whenever Valid is false.
type Certificates struct {
	Certificates       [][]byte
	ParsedCertificates []*x509.Certificate
}

// ServerKeyExchange represents the raw key data sent by the server in TLS key exchange message
type ServerKeyExchange struct {
	Key []byte `json:"key"`
}

// Finished represents a TLS Finished message
type Finished struct {
	VerifyData []byte `json:"verify_data"`
}

// ServerHandshake stores all of the messages sent by the server during a standard TLS Handshake.
// It implements zgrab.EventData interface
type ServerHandshake struct {
	ServerHello        *ServerHello       `json:"server_hello"`
	ServerCertificates *Certificates      `json:"server_certificates"`
	ServerKeyExchange  *ServerKeyExchange `json:"server_key_exchange"`
	RSAExportParams    *RSAExportParams   `json:"rsa_export_params,omitempty"`
	DHExportParams     *DHParams          `json:"dh_export_params,omitempty"`
	DHParams           *DHParams          `json:"dh_params,omitempty"`
	ServerFinished     *Finished          `json:"server_finished"`
}

func (hs *ServerHandshake) setSkx(skx *serverKeyExchangeMsg, cipher uint16) {
	hs.ServerKeyExchange = skx.MakeLog()
	// Check the cipher suite to see if it's RSA or DHE
	if cipherInList(cipher, RSAExportCiphers) {
		var p rsaExportParams
		if p.unmarshal(skx.key) {
			hs.RSAExportParams = p.MakeLog()
		}
	} else if cipherInList(cipher, DHEExportCiphers) {
		p := new(DHParams)
		if p.unmarshal(skx.key) {
			hs.DHExportParams = p
		}
	} else if cipherInList(cipher, DHECiphers) {
		p := new(DHParams)
		if p.unmarshal(skx.key) {
			hs.DHParams = p
		}
	}
}

func (c *Conn) GetHandshakeLog() *ServerHandshake {
	return c.handshakeLog
}

func (m *serverHelloMsg) MakeLog() *ServerHello {
	sh := new(ServerHello)
	sh.Version = TLSVersion(m.vers)
	sh.Random = make([]byte, len(m.random))
	copy(sh.Random, m.random)
	sh.SessionID = make([]byte, len(m.sessionId))
	copy(sh.SessionID, m.sessionId)
	sh.CipherSuite = CipherSuite(m.cipherSuite)
	sh.CompressionMethod = m.compressionMethod
	sh.OcspStapling = m.ocspStapling
	sh.TicketSupported = m.ticketSupported
	sh.SecureRenegotiation = m.secureRenegotiation
	sh.HeartbeatSupported = m.heartbeatEnabled
	return sh
}

func (m *certificateMsg) MakeLog() *Certificates {
	sc := new(Certificates)
	sc.Certificates = make([][]byte, len(m.certificates))
	for idx, cert := range m.certificates {
		sc.Certificates[idx] = make([]byte, len(cert))
		copy(sc.Certificates[idx], cert)
	}
	return sc
}

func (m *serverKeyExchangeMsg) MakeLog() *ServerKeyExchange {
	skx := new(ServerKeyExchange)
	skx.Key = make([]byte, len(m.key))
	copy(skx.Key, m.key)
	return skx
}

func (m *finishedMsg) MakeLog() *Finished {
	sf := new(Finished)
	sf.VerifyData = make([]byte, len(m.verifyData))
	copy(sf.VerifyData, m.verifyData)
	return sf
}

type ExportSignatureAlgorithm uint16

var exportHashes = []string{
	"MD5",
	"SHA-1",
	"SHA-224",
	"SHA-256",
	"SHA-384",
	"SHA-512",
}

var exportAlgorithms = []string{
	"anon",
	"RSA",
	"DSA",
	"ECDSA",
}

func (esa *ExportSignatureAlgorithm) MarshalJSON() ([]byte, error) {
	value := uint16(*esa)
	hash := int(byte(value >> 8))
	alg := int(byte(value))

	var aux struct {
		Value    uint16 `json:"value"`
		HashName string `json:"hash_name,omitempty"`
		HashID   int    `json:"hash_id"`
		AlgName  string `json:"algorithm_name,omitempty"`
		AlgID    int    `json:"algorithm_id"`
	}
	if hash < len(exportHashes) {
		aux.HashName = exportHashes[hash]
	}
	if alg < len(exportAlgorithms) {
		aux.AlgName = exportAlgorithms[alg]
	}
	aux.Value = value
	aux.HashID = hash
	aux.AlgID = alg
	return json.Marshal(&aux)
}

type RSAExportParams struct {
	PublicKey rsa.PublicKey `json:"-"`
	Modulus   []byte        `json:"modulus"`
	Exponent  uint32        `json:"exponent"`
	Length    int           `json:"length"`
}

type DHParams struct {
	P  *big.Int
	G  *big.Int
	Ys *big.Int
}

func (p *DHParams) MarshalJSON() ([]byte, error) {
	var aux struct {
		P        []byte `json:"prime"`
		PrimeLen int    `json:"prime_length"`
		G        []byte `json:"generator"`
		Ys       []byte `json:"public_exponent"`
	}
	aux.P = p.P.Bytes()
	aux.PrimeLen = p.P.BitLen()
	aux.G = p.G.Bytes()
	aux.Ys = p.Ys.Bytes()
	return json.Marshal(&aux)
}

func (p *rsaExportParams) MakeLog() *RSAExportParams {
	out := new(RSAExportParams)
	exponent := uint32(0)
	for _, b := range p.rawExponent {
		exponent <<= 8
		exponent |= uint32(b)
	}
	modulus := big.NewInt(0)
	modulus.SetBytes(p.rawModulus)
	key := rsa.PublicKey{
		N: modulus,
		E: int(exponent),
	}
	out.PublicKey = key
	out.Modulus = modulus.Bytes()
	out.Exponent = exponent
	out.Length = modulus.BitLen()
	return out
}

func (p *DHParams) unmarshal(buf []byte) bool {
	if len(buf) < 2 {
		return false
	}
	pLength := binary.BigEndian.Uint16(buf)
	buf = buf[2:]

	if len(buf) < int(pLength) {
		return false
	}
	pBytes := buf[0:pLength]
	buf = buf[pLength:]
	p.P = big.NewInt(0)
	p.P.SetBytes(pBytes)

	if len(buf) < 2 {
		return false
	}
	gLength := binary.BigEndian.Uint16(buf)
	buf = buf[2:]

	if len(buf) < int(gLength) {
		return false
	}
	gBytes := buf[0:gLength]
	buf = buf[gLength:]
	p.G = big.NewInt(0)
	p.G.SetBytes(gBytes)

	if len(buf) < 2 {
		return false
	}
	ysLength := binary.BigEndian.Uint16(buf)
	buf = buf[2:]

	if len(buf) < int(ysLength) {
		return false
	}
	ysBytes := buf[0:ysLength]
	p.Ys = big.NewInt(0)
	p.Ys.SetBytes(ysBytes)

	return true
}
