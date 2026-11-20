package importer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chapar-rest/chapar/internal/domain"
	"github.com/chapar-rest/chapar/internal/repository"
)

const sampleCalculatorWSDL = `<?xml version="1.0" encoding="utf-8"?>
<wsdl:definitions xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
                  xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"
                  xmlns:tns="http://tempuri.org/"
                  xmlns:s="http://www.w3.org/2001/XMLSchema"
                  xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"
                  targetNamespace="http://tempuri.org/">
  <wsdl:types>
    <s:schema elementFormDefault="qualified" targetNamespace="http://tempuri.org/">
      <s:element name="Add">
        <s:complexType>
          <s:sequence>
            <s:element minOccurs="1" maxOccurs="1" name="intA" type="s:int" />
            <s:element minOccurs="1" maxOccurs="1" name="intB" type="s:int" />
          </s:sequence>
        </s:complexType>
      </s:element>
      <s:element name="AddResponse">
        <s:complexType>
          <s:sequence>
            <s:element minOccurs="1" maxOccurs="1" name="AddResult" type="s:int" />
          </s:sequence>
        </s:complexType>
      </s:element>
    </s:schema>
  </wsdl:types>
  <wsdl:message name="AddSoapIn">
    <wsdl:part name="parameters" element="tns:Add" />
  </wsdl:message>
  <wsdl:message name="AddSoapOut">
    <wsdl:part name="parameters" element="tns:AddResponse" />
  </wsdl:message>
  <wsdl:portType name="CalculatorSoap">
    <wsdl:operation name="Add">
      <wsdl:documentation>Adds two integers.</wsdl:documentation>
      <wsdl:input message="tns:AddSoapIn" />
      <wsdl:output message="tns:AddSoapOut" />
    </wsdl:operation>
  </wsdl:portType>
  <wsdl:binding name="CalculatorSoap" type="tns:CalculatorSoap">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http" />
    <wsdl:operation name="Add">
      <soap:operation soapAction="http://tempuri.org/Add" style="document" />
      <wsdl:input>
        <soap:body use="literal" />
      </wsdl:input>
      <wsdl:output>
        <soap:body use="literal" />
      </wsdl:output>
    </wsdl:operation>
  </wsdl:binding>
  <wsdl:binding name="CalculatorSoap12" type="tns:CalculatorSoap">
    <soap12:binding transport="http://schemas.xmlsoap.org/soap/http" />
    <wsdl:operation name="Add">
      <soap12:operation soapAction="http://tempuri.org/Add" style="document" />
      <wsdl:input>
        <soap12:body use="literal" />
      </wsdl:input>
      <wsdl:output>
        <soap12:body use="literal" />
      </wsdl:output>
    </wsdl:operation>
  </wsdl:binding>
  <wsdl:service name="Calculator">
    <wsdl:port name="CalculatorSoap" binding="tns:CalculatorSoap">
      <soap:address location="http://www.dneonline.com/calculator.asmx" />
    </wsdl:port>
    <wsdl:port name="CalculatorSoap12" binding="tns:CalculatorSoap12">
      <soap12:address location="http://www.dneonline.com/calculator.asmx" />
    </wsdl:port>
  </wsdl:service>
</wsdl:definitions>`

func TestImportWSDL_SOAP11And12(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "chapar_wsdl_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	repo, err := repository.NewFilesystemV2(tmpDir, "Default")
	require.NoError(t, err)

	err = ImportWSDL([]byte(sampleCalculatorWSDL), repo, "calculator.wsdl")
	require.NoError(t, err)

	cols, err := repo.LoadCollections()
	require.NoError(t, err)
	require.NotEmpty(t, cols)

	reqs := cols[0].Spec.Requests
	require.NotEmpty(t, reqs)

	// We expect requests generated for the operations
	var foundSoap11, foundSoap12 bool
	for _, r := range reqs {
		assert.Equal(t, domain.RequestTypeHTTP, r.MetaData.Type)
		assert.Equal(t, "POST", r.Spec.HTTP.Method)
		assert.Equal(t, "http://www.dneonline.com/calculator.asmx", r.Spec.HTTP.URL)
		assert.Equal(t, domain.RequestBodyTypeXML, r.Spec.HTTP.Request.Body.Type)

		headers := make(map[string]string)
		for _, h := range r.Spec.HTTP.Request.Headers {
			headers[h.Key] = h.Value
		}

		if strings.Contains(headers["Content-Type"], "text/xml") {
			foundSoap11 = true
			assert.Equal(t, "http://tempuri.org/Add", headers["SOAPAction"])
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "http://schemas.xmlsoap.org/soap/envelope/")
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "<intA>?</intA>")
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "<intB>?</intB>")
		} else if strings.Contains(headers["Content-Type"], "application/soap+xml") {
			foundSoap12 = true
			assert.Contains(t, headers["Content-Type"], `action="http://tempuri.org/Add"`)
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "http://www.w3.org/2003/05/soap-envelope")
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "<intA>?</intA>")
			assert.Contains(t, r.Spec.HTTP.Request.Body.Data, "<intB>?</intB>")
		}
	}

	assert.True(t, foundSoap11, "SOAP 1.1 request should be generated")
	assert.True(t, foundSoap12, "SOAP 1.2 request should be generated")
}

func TestImportWSDL_FromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(sampleCalculatorWSDL))
	}))
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "chapar_wsdl_url_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	repo, err := repository.NewFilesystemV2(tmpDir, "Default")
	require.NoError(t, err)

	err = ImportWSDLFromURL(ts.URL+"?wsdl", repo)
	require.NoError(t, err)

	cols, err := repo.LoadCollections()
	require.NoError(t, err)
	require.NotEmpty(t, cols)
}

func TestImportWSDL_ComplexNestedTypes(t *testing.T) {
	complexWSDL := `<?xml version="1.0" encoding="utf-8"?>
<definitions xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
             xmlns:tns="http://example.com/orders"
             xmlns:xsd="http://www.w3.org/2001/XMLSchema"
             xmlns="http://schemas.xmlsoap.org/wsdl/"
             targetNamespace="http://example.com/orders">
  <types>
    <xsd:schema targetNamespace="http://example.com/orders">
      <xsd:complexType name="Item">
        <xsd:sequence>
          <xsd:element name="productId" type="xsd:string"/>
          <xsd:element name="quantity" type="xsd:int"/>
        </xsd:sequence>
      </xsd:complexType>
      <xsd:element name="CreateOrder">
        <xsd:complexType>
          <xsd:sequence>
            <xsd:element name="orderId" type="xsd:string"/>
            <xsd:element name="item" type="tns:Item"/>
          </xsd:sequence>
        </xsd:complexType>
      </xsd:element>
    </xsd:schema>
  </types>
  <message name="CreateOrderIn">
    <part name="parameters" element="tns:CreateOrder"/>
  </message>
  <portType name="OrderPortType">
    <operation name="CreateOrder">
      <input message="tns:CreateOrderIn"/>
    </operation>
  </portType>
  <binding name="OrderSoapBinding" type="tns:OrderPortType">
    <soap:binding transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="CreateOrder">
      <soap:operation soapAction="http://example.com/orders/CreateOrder"/>
      <input><soap:body use="literal"/></input>
    </operation>
  </binding>
  <service name="OrderService">
    <port name="OrderPort" binding="tns:OrderSoapBinding">
      <soap:address location="http://example.com/orders/api"/>
    </port>
  </service>
</definitions>`

	tmpDir, err := os.MkdirTemp("", "chapar_wsdl_complex_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	repo, err := repository.NewFilesystemV2(tmpDir, "Default")
	require.NoError(t, err)

	err = ImportWSDL([]byte(complexWSDL), repo, "order.wsdl")
	require.NoError(t, err)

	cols, err := repo.LoadCollections()
	require.NoError(t, err)
	require.NotEmpty(t, cols)

	reqs := cols[0].Spec.Requests
	require.Len(t, reqs, 1)

	body := reqs[0].Spec.HTTP.Request.Body.Data
	assert.Contains(t, body, "<orderId>?</orderId>")
	assert.Contains(t, body, "<productId>?</productId>")
	assert.Contains(t, body, "<quantity>?</quantity>")
}

func TestImportWSDL_RPCStyle(t *testing.T) {
	rpcWSDL := `<?xml version="1.0" encoding="utf-8"?>
<definitions xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
             xmlns:tns="http://example.com/rpc"
             xmlns:xsd="http://www.w3.org/2001/XMLSchema"
             xmlns="http://schemas.xmlsoap.org/wsdl/"
             targetNamespace="http://example.com/rpc">
  <message name="GetUserIn">
    <part name="userId" type="xsd:string"/>
    <part name="includeDetails" type="xsd:boolean"/>
  </message>
  <portType name="UserPortType">
    <operation name="GetUser">
      <input message="tns:GetUserIn"/>
    </operation>
  </portType>
  <binding name="UserSoapBinding" type="tns:UserPortType">
    <soap:binding style="rpc" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="GetUser">
      <soap:operation soapAction="http://example.com/rpc/GetUser"/>
      <input><soap:body use="literal"/></input>
    </operation>
  </binding>
  <service name="UserService">
    <port name="UserPort" binding="tns:UserSoapBinding">
      <soap:address location="http://example.com/rpc/user"/>
    </port>
  </service>
</definitions>`

	tmpDir, err := os.MkdirTemp("", "chapar_wsdl_rpc_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	repo, err := repository.NewFilesystemV2(tmpDir, "Default")
	require.NoError(t, err)

	err = ImportWSDL([]byte(rpcWSDL), repo, "user.wsdl")
	require.NoError(t, err)

	cols, err := repo.LoadCollections()
	require.NoError(t, err)
	require.NotEmpty(t, cols)

	reqs := cols[0].Spec.Requests
	require.Len(t, reqs, 1)

	body := reqs[0].Spec.HTTP.Request.Body.Data
	assert.Contains(t, body, "<tns:GetUser>")
	assert.Contains(t, body, "<userId>?</userId>")
	assert.Contains(t, body, "<includeDetails>?</includeDetails>")
}

func TestImportWSDL_Errors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "chapar_wsdl_err_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	repo, err := repository.NewFilesystemV2(tmpDir, "Default")
	require.NoError(t, err)

	// Invalid XML
	err = ImportWSDL([]byte("not xml"), repo, "invalid.wsdl")
	assert.Error(t, err)

	// Valid XML but not WSDL
	err = ImportWSDL([]byte("<root><child/></root>"), repo, "empty.wsdl")
	assert.Error(t, err)
}
