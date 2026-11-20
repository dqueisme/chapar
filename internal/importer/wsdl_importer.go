package importer

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chapar-rest/chapar/internal/domain"
	"github.com/chapar-rest/chapar/internal/repository"
)

// WSDL 1.1 and SOAP XML Namespaces
const (
	NamespaceSOAP11 = "http://schemas.xmlsoap.org/wsdl/soap/"
	NamespaceSOAP12 = "http://schemas.xmlsoap.org/wsdl/soap12/"
	EnvelopeSOAP11  = "http://schemas.xmlsoap.org/soap/envelope/"
	EnvelopeSOAP12  = "http://www.w3.org/2003/05/soap-envelope"
)

// WSDL Definitions model
type wsdlDefinitions struct {
	XMLName         xml.Name       `xml:"definitions"`
	Name            string         `xml:"name,attr"`
	TargetNamespace string         `xml:"targetNamespace,attr"`
	Types           wsdlTypes      `xml:"types"`
	Messages        []wsdlMessage  `xml:"message"`
	PortTypes       []wsdlPortType `xml:"portType"`
	Bindings        []wsdlBinding  `xml:"binding"`
	Services        []wsdlService  `xml:"service"`
}

type wsdlTypes struct {
	Schemas []xsdSchema `xml:"schema"`
}

type xsdSchema struct {
	TargetNamespace string           `xml:"targetNamespace,attr"`
	Elements        []xsdElement     `xml:"element"`
	ComplexTypes    []xsdComplexType `xml:"complexType"`
	Imports         []xsdImport      `xml:"import"`
}

type xsdImport struct {
	Namespace      string `xml:"namespace,attr"`
	SchemaLocation string `xml:"schemaLocation,attr"`
}

type xsdElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	ComplexType *xsdComplexType `xml:"complexType"`
}

type xsdComplexType struct {
	Name           string             `xml:"name,attr"`
	Sequence       *xsdSequence       `xml:"sequence"`
	All            *xsdSequence       `xml:"all"`
	Choice         *xsdSequence       `xml:"choice"`
	ComplexContent *xsdComplexContent `xml:"complexContent"`
}

type xsdComplexContent struct {
	Extension *xsdExtension `xml:"extension"`
}

type xsdExtension struct {
	Base     string       `xml:"base,attr"`
	Sequence *xsdSequence `xml:"sequence"`
}

type xsdSequence struct {
	Elements []xsdElement `xml:"element"`
}

type wsdlMessage struct {
	Name  string            `xml:"name,attr"`
	Parts []wsdlMessagePart `xml:"part"`
}

type wsdlMessagePart struct {
	Name    string `xml:"name,attr"`
	Element string `xml:"element,attr"`
	Type    string `xml:"type,attr"`
}

type wsdlPortType struct {
	Name       string              `xml:"name,attr"`
	Operations []wsdlPortOperation `xml:"operation"`
}

type wsdlPortOperation struct {
	Name   string           `xml:"name,attr"`
	Input  *wsdlOperationIO `xml:"input"`
	Output *wsdlOperationIO `xml:"output"`
}

type wsdlOperationIO struct {
	Name    string `xml:"name,attr"`
	Message string `xml:"message,attr"`
}

type wsdlBinding struct {
	Name           string                 `xml:"name,attr"`
	Type           string                 `xml:"type,attr"`
	SOAP11Binding  *wsdlSOAPBinding       `xml:"http://schemas.xmlsoap.org/wsdl/soap/ binding"`
	SOAP12Binding  *wsdlSOAPBinding       `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ binding"`
	GenericBinding *wsdlSOAPBinding       `xml:"binding"`
	Operations     []wsdlBindingOperation `xml:"operation"`
}

type wsdlSOAPBinding struct {
	Transport string `xml:"transport,attr"`
	Style     string `xml:"style,attr"`
}

type wsdlBindingOperation struct {
	Name             string             `xml:"name,attr"`
	SOAP11Operation  *wsdlSOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap/ operation"`
	SOAP12Operation  *wsdlSOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ operation"`
	GenericOperation *wsdlSOAPOperation `xml:"operation"`
	Input            *wsdlBindingIO     `xml:"input"`
	Output           *wsdlBindingIO     `xml:"output"`
}

type wsdlSOAPOperation struct {
	SOAPAction string `xml:"soapAction,attr"`
	Style      string `xml:"style,attr"`
}

type wsdlBindingIO struct {
	SOAP11Body *wsdlSOAPBody `xml:"http://schemas.xmlsoap.org/wsdl/soap/ body"`
	SOAP12Body *wsdlSOAPBody `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ body"`
	Body       *wsdlSOAPBody `xml:"body"`
}

type wsdlSOAPBody struct {
	Use       string `xml:"use,attr"`
	Namespace string `xml:"namespace,attr"`
}

type wsdlService struct {
	Name  string            `xml:"name,attr"`
	Ports []wsdlServicePort `xml:"port"`
}

type wsdlServicePort struct {
	Name          string           `xml:"name,attr"`
	Binding       string           `xml:"binding,attr"`
	SOAP11Address *wsdlSOAPAddress `xml:"http://schemas.xmlsoap.org/wsdl/soap/ address"`
	SOAP12Address *wsdlSOAPAddress `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ address"`
	Address       *wsdlSOAPAddress `xml:"address"`
}

type wsdlSOAPAddress struct {
	Location string `xml:"location,attr"`
}

// ImportWSDLFromURL fetches a WSDL definition from an HTTP/HTTPS URL and imports it.
func ImportWSDLFromURL(wsdlURL string, repo repository.RepositoryV2) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(wsdlURL)
	if err != nil {
		return fmt.Errorf("failed to fetch WSDL from %s: %w", wsdlURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fetching WSDL returned HTTP status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read WSDL response body: %w", err)
	}

	return ImportWSDL(data, repo, wsdlURL)
}

// ImportWSDLFromFile reads a local WSDL file and imports it.
func ImportWSDLFromFile(filePath string, repo repository.RepositoryV2) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read WSDL file %s: %w", filePath, err)
	}
	return ImportWSDL(data, repo, filePath)
}

// ImportWSDL parses WSDL bytes and imports operations as Chapar HTTP requests.
func ImportWSDL(data []byte, repo repository.RepositoryV2, sourcePathOrURL string) error {
	var def wsdlDefinitions
	if err := xml.Unmarshal(data, &def); err != nil {
		return fmt.Errorf("failed to parse WSDL XML: %w", err)
	}

	if len(def.PortTypes) == 0 && len(def.Services) == 0 {
		return fmt.Errorf("no portTypes or services found in WSDL")
	}

	// Index schemas
	elementsMap := make(map[string]xsdElement)
	complexTypesMap := make(map[string]xsdComplexType)

	// Ingest external schemas if present
	for _, schema := range def.Types.Schemas {
		indexSchema(schema, elementsMap, complexTypesMap)
		for _, imp := range schema.Imports {
			if imp.SchemaLocation != "" {
				loadExternalSchema(imp.SchemaLocation, sourcePathOrURL, elementsMap, complexTypesMap)
			}
		}
	}

	// Index messages
	messagesMap := make(map[string]wsdlMessage)
	for _, msg := range def.Messages {
		messagesMap[msg.Name] = msg
	}

	// Index portTypes
	portTypesMap := make(map[string]wsdlPortType)
	for _, pt := range def.PortTypes {
		portTypesMap[pt.Name] = pt
	}

	// Index bindings
	bindingsMap := make(map[string]wsdlBinding)
	for _, b := range def.Bindings {
		bindingsMap[b.Name] = b
	}

	// Target collection name
	collectionName := def.Name
	if collectionName == "" && len(def.Services) > 0 {
		collectionName = def.Services[0].Name
	}
	if collectionName == "" {
		collectionName = "WSDL Service"
	}

	col := domain.NewCollection(collectionName)
	if err := repo.CreateCollection(col); err != nil {
		return fmt.Errorf("error creating collection: %w", err)
	}

	var requests []*domain.Request

	// If services exist, iterate ports
	if len(def.Services) > 0 {
		for _, service := range def.Services {
			for _, port := range service.Ports {
				bindingName := stripPrefix(port.Binding)
				binding, ok := bindingsMap[bindingName]
				if !ok {
					continue
				}

				endpointURL := ""
				if port.SOAP11Address != nil && port.SOAP11Address.Location != "" {
					endpointURL = port.SOAP11Address.Location
				} else if port.SOAP12Address != nil && port.SOAP12Address.Location != "" {
					endpointURL = port.SOAP12Address.Location
				} else if port.Address != nil && port.Address.Location != "" {
					endpointURL = port.Address.Location
				}

				isSOAP12 := binding.SOAP12Binding != nil || port.SOAP12Address != nil
				reqs := generateRequestsForBinding(binding, portTypesMap, messagesMap, elementsMap, complexTypesMap, def.TargetNamespace, endpointURL, isSOAP12, port.Name)
				requests = append(requests, reqs...)
			}
		}
	} else {
		// No service block: generate from bindings directly
		for _, binding := range def.Bindings {
			isSOAP12 := binding.SOAP12Binding != nil
			reqs := generateRequestsForBinding(binding, portTypesMap, messagesMap, elementsMap, complexTypesMap, def.TargetNamespace, "http://localhost/soap", isSOAP12, binding.Name)
			requests = append(requests, reqs...)
		}
	}

	for _, req := range requests {
		if err := repo.CreateRequest(req, col); err != nil {
			return fmt.Errorf("error creating request: %w", err)
		}
	}

	return nil
}

func indexSchema(schema xsdSchema, elementsMap map[string]xsdElement, complexTypesMap map[string]xsdComplexType) {
	for _, el := range schema.Elements {
		elementsMap[el.Name] = el
	}
	for _, ct := range schema.ComplexTypes {
		complexTypesMap[ct.Name] = ct
	}
}

func loadExternalSchema(schemaLocation, baseLocation string, elementsMap map[string]xsdElement, complexTypesMap map[string]xsdComplexType) {
	var data []byte
	if strings.HasPrefix(schemaLocation, "http://") || strings.HasPrefix(schemaLocation, "https://") {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(schemaLocation)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		data, _ = io.ReadAll(resp.Body)
	} else if strings.HasPrefix(baseLocation, "http://") || strings.HasPrefix(baseLocation, "https://") {
		baseURL, err := url.Parse(baseLocation)
		if err != nil {
			return
		}
		relURL, err := url.Parse(schemaLocation)
		if err != nil {
			return
		}
		resolved := baseURL.ResolveReference(relURL).String()
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(resolved)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		data, _ = io.ReadAll(resp.Body)
	} else if baseLocation != "" {
		filePath := filepath.Join(filepath.Dir(baseLocation), schemaLocation)
		data, _ = os.ReadFile(filePath)
	}

	if len(data) > 0 {
		var schema xsdSchema
		if err := xml.Unmarshal(data, &schema); err == nil {
			indexSchema(schema, elementsMap, complexTypesMap)
		}
	}
}

func generateRequestsForBinding(
	binding wsdlBinding,
	portTypesMap map[string]wsdlPortType,
	messagesMap map[string]wsdlMessage,
	elementsMap map[string]xsdElement,
	complexTypesMap map[string]xsdComplexType,
	targetNamespace string,
	endpointURL string,
	isSOAP12 bool,
	portOrBindingName string,
) []*domain.Request {
	portTypeName := stripPrefix(binding.Type)
	portType, ok := portTypesMap[portTypeName]
	if !ok {
		return nil
	}

	// Map binding operations by name
	bindingOps := make(map[string]wsdlBindingOperation)
	for _, op := range binding.Operations {
		bindingOps[op.Name] = op
	}

	var results []*domain.Request

	for _, ptOp := range portType.Operations {
		bOp, hasBOp := bindingOps[ptOp.Name]
		soapAction := ""
		if hasBOp {
			if isSOAP12 && bOp.SOAP12Operation != nil {
				soapAction = bOp.SOAP12Operation.SOAPAction
			} else if bOp.SOAP11Operation != nil {
				soapAction = bOp.SOAP11Operation.SOAPAction
			} else if bOp.GenericOperation != nil {
				soapAction = bOp.GenericOperation.SOAPAction
			}
		}

		bodyContent := ""
		if ptOp.Input != nil {
			inputMsgName := stripPrefix(ptOp.Input.Message)
			inputMsg, hasMsg := messagesMap[inputMsgName]
			if hasMsg {
				bodyContent = generateMessageBody(inputMsg, ptOp.Name, elementsMap, complexTypesMap, targetNamespace)
			}
		}

		fullEnvelope := ""
		headers := []domain.KeyValue{}

		if isSOAP12 {
			fullEnvelope = fmt.Sprintf(
				"<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<soap12:Envelope xmlns:soap12=\"%s\" xmlns:tns=\"%s\">\n  <soap12:Header/>\n  <soap12:Body>\n%s\n  </soap12:Body>\n</soap12:Envelope>",
				EnvelopeSOAP12,
				targetNamespace,
				indentString(bodyContent, "    "),
			)

			contentType := "application/soap+xml; charset=utf-8"
			if soapAction != "" {
				contentType = fmt.Sprintf("application/soap+xml; charset=utf-8; action=\"%s\"", soapAction)
			}
			headers = append(headers, domain.KeyValue{
				ID:     uuid.NewString(),
				Key:    "Content-Type",
				Value:  contentType,
				Enable: true,
			})
		} else {
			fullEnvelope = fmt.Sprintf(
				"<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<soapenv:Envelope xmlns:soapenv=\"%s\" xmlns:tns=\"%s\">\n  <soapenv:Header/>\n  <soapenv:Body>\n%s\n  </soapenv:Body>\n</soapenv:Envelope>",
				EnvelopeSOAP11,
				targetNamespace,
				indentString(bodyContent, "    "),
			)

			headers = append(headers, domain.KeyValue{
				ID:     uuid.NewString(),
				Key:    "Content-Type",
				Value:  "text/xml; charset=utf-8",
				Enable: true,
			})
			if soapAction != "" {
				headers = append(headers, domain.KeyValue{
					ID:     uuid.NewString(),
					Key:    "SOAPAction",
					Value:  soapAction,
					Enable: true,
				})
			}
		}

		reqName := ptOp.Name
		if strings.Contains(strings.ToLower(portOrBindingName), "12") || isSOAP12 {
			reqName = fmt.Sprintf("%s (SOAP 1.2)", ptOp.Name)
		} else if strings.Contains(strings.ToLower(portOrBindingName), "soap") {
			reqName = fmt.Sprintf("%s (SOAP 1.1)", ptOp.Name)
		}

		req := &domain.Request{
			ApiVersion: "v1",
			Kind:       "Request",
			MetaData: domain.RequestMeta{
				ID:   uuid.NewString(),
				Name: reqName,
				Type: domain.RequestTypeHTTP,
			},
			Spec: domain.RequestSpec{
				HTTP: &domain.HTTPRequestSpec{
					Method: domain.RequestMethodPOST,
					URL:    endpointURL,
					Request: &domain.HTTPRequest{
						Headers:     headers,
						PathParams:  []domain.KeyValue{},
						QueryParams: []domain.KeyValue{},
						Body: domain.Body{
							Type: domain.RequestBodyTypeXML,
							Data: fullEnvelope,
						},
					},
				},
			},
		}

		req.SetDefaultValues()
		results = append(results, req)
	}

	return results
}

func generateMessageBody(
	msg wsdlMessage,
	operationName string,
	elementsMap map[string]xsdElement,
	complexTypesMap map[string]xsdComplexType,
	targetNamespace string,
) string {
	var parts []string
	for _, part := range msg.Parts {
		if part.Element != "" {
			elName := stripPrefix(part.Element)
			if el, ok := elementsMap[elName]; ok {
				parts = append(parts, renderElement(el, complexTypesMap, "tns", 0))
			} else {
				parts = append(parts, fmt.Sprintf("<tns:%s>?</tns:%s>", elName, elName))
			}
		} else if part.Type != "" {
			// RPC Style part
			parts = append(parts, fmt.Sprintf("<%s>?</%s>", part.Name, part.Name))
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("<tns:%s/>", operationName)
	}

	// If RPC style with type attributes, wrap with operation name
	if len(msg.Parts) > 0 && msg.Parts[0].Type != "" {
		return fmt.Sprintf("<tns:%s>\n%s\n</tns:%s>", operationName, indentString(strings.Join(parts, "\n"), "  "), operationName)
	}

	return strings.Join(parts, "\n")
}

func renderElement(
	el xsdElement,
	complexTypesMap map[string]xsdComplexType,
	nsPrefix string,
	depth int,
) string {
	if depth > 6 {
		return fmt.Sprintf("<%s:%s>?</%s:%s>", nsPrefix, el.Name, nsPrefix, el.Name)
	}

	prefixTag := el.Name
	if nsPrefix != "" {
		prefixTag = fmt.Sprintf("%s:%s", nsPrefix, el.Name)
	}

	// 1. Inlined complexType
	if el.ComplexType != nil {
		inner := renderComplexType(*el.ComplexType, complexTypesMap, nsPrefix, depth+1)
		if inner == "" {
			return fmt.Sprintf("<%s/>", prefixTag)
		}
		return fmt.Sprintf("<%s>\n%s\n</%s>", prefixTag, indentString(inner, "  "), prefixTag)
	}

	// 2. Referenced complexType
	if el.Type != "" {
		typeName := stripPrefix(el.Type)
		if ct, ok := complexTypesMap[typeName]; ok {
			inner := renderComplexType(ct, complexTypesMap, nsPrefix, depth+1)
			if inner == "" {
				return fmt.Sprintf("<%s/>", prefixTag)
			}
			return fmt.Sprintf("<%s>\n%s\n</%s>", prefixTag, indentString(inner, "  "), prefixTag)
		}
	}

	// 3. Simple leaf type
	return fmt.Sprintf("<%s>?</%s>", prefixTag, prefixTag)
}

func renderComplexType(
	ct xsdComplexType,
	complexTypesMap map[string]xsdComplexType,
	nsPrefix string,
	depth int,
) string {
	var seqElements []xsdElement
	if ct.Sequence != nil {
		seqElements = append(seqElements, ct.Sequence.Elements...)
	}
	if ct.All != nil {
		seqElements = append(seqElements, ct.All.Elements...)
	}
	if ct.Choice != nil {
		seqElements = append(seqElements, ct.Choice.Elements...)
	}
	if ct.ComplexContent != nil && ct.ComplexContent.Extension != nil {
		baseName := stripPrefix(ct.ComplexContent.Extension.Base)
		if baseCt, ok := complexTypesMap[baseName]; ok {
			baseRendered := renderComplexType(baseCt, complexTypesMap, nsPrefix, depth+1)
			if baseRendered != "" {
				return baseRendered
			}
		}
		if ct.ComplexContent.Extension.Sequence != nil {
			seqElements = append(seqElements, ct.ComplexContent.Extension.Sequence.Elements...)
		}
	}

	var rendered []string
	for _, child := range seqElements {
		rendered = append(rendered, renderElement(child, complexTypesMap, "", depth+1))
	}

	return strings.Join(rendered, "\n")
}

func stripPrefix(val string) string {
	if idx := strings.Index(val, ":"); idx != -1 {
		return val[idx+1:]
	}
	return val
}

func indentString(s, indent string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = indent + l
		}
	}
	return strings.Join(lines, "\n")
}
