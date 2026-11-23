package nokogiri_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/base64"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/digest"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/nethttp"
	_ "github.com/oruby/oruby/gem/nokogiri"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/time"
)

func TestEInvoiceXSDValidationAndXSLT(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "nokogiri"

# 1. XSD 1.0 schema (UBL-like e-Invoice structure)
xsd_src = <<~XSD
  <?xml version="1.0" encoding="UTF-8"?>
  <xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
             targetNamespace="urn:example:einvoice:1"
             xmlns:inv="urn:example:einvoice:1"
             elementFormDefault="qualified">
    <xs:element name="Invoice" type="inv:InvoiceType"/>
    <xs:complexType name="InvoiceType">
      <xs:sequence>
        <xs:element name="ID" type="xs:string"/>
        <xs:element name="IssueDate" type="xs:date"/>
        <xs:element name="SupplierVAT" type="xs:string"/>
        <xs:element name="Line" type="inv:LineType" minOccurs="1" maxOccurs="unbounded"/>
        <xs:element name="PayableAmount" type="inv:AmountType"/>
      </xs:sequence>
    </xs:complexType>
    <xs:complexType name="LineType">
      <xs:sequence>
        <xs:element name="Description" type="xs:string"/>
        <xs:element name="Amount" type="inv:AmountType"/>
      </xs:sequence>
    </xs:complexType>
    <xs:complexType name="AmountType">
      <xs:simpleContent>
        <xs:extension base="xs:decimal">
          <xs:attribute name="currencyID" type="xs:string" use="required"/>
        </xs:extension>
      </xs:simpleContent>
    </xs:complexType>
  </xs:schema>
XSD

valid_xml = <<~XML
  <?xml version="1.0" encoding="UTF-8"?>
  <Invoice xmlns="urn:example:einvoice:1">
    <ID>INV-2026-0001</ID>
    <IssueDate>2026-11-23</IssueDate>
    <SupplierVAT>HR12345678901</SupplierVAT>
    <Line>
      <Description>Cloud Hosting</Description>
      <Amount currencyID="EUR">100.00</Amount>
    </Line>
    <Line>
      <Description>Support Plan</Description>
      <Amount currencyID="EUR">25.00</Amount>
    </Line>
    <PayableAmount currencyID="EUR">125.00</PayableAmount>
  </Invoice>
XML

invalid_xml = <<~XML
  <?xml version="1.0" encoding="UTF-8"?>
  <Invoice xmlns="urn:example:einvoice:1">
    <ID>INV-BAD</ID>
    <IssueDate>not-a-date</IssueDate>
    <SupplierVAT>HR12345678901</SupplierVAT>
    <PayableAmount>not-a-number</PayableAmount>
  </Invoice>
XML

schema = Nokogiri::XML::Schema(xsd_src)
doc = Nokogiri::XML(valid_xml)
errs = schema.validate(doc)
raise "expected valid invoice, got: #{errs.map(&:message).inspect}" unless errs.empty? && schema.valid?(doc)

bad_doc = Nokogiri::XML(invalid_xml)
bad_errs = schema.validate(bad_doc)
raise "expected invalid invoice errors" if bad_errs.empty? || schema.valid?(bad_doc)

# 2. XSLT 1.0 transformation (e-Invoice XML -> HTML with quote_params)
xsl1 = <<~XSL
  <?xml version="1.0" encoding="UTF-8"?>
  <xsl:stylesheet version="1.0"
      xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
      xmlns:inv="urn:example:einvoice:1">
    <xsl:output method="html" encoding="UTF-8"/>
    <xsl:param name="banner" select="'Standard'"/>
    <xsl:template match="/inv:Invoice">
      <html>
        <body>
          <h1><xsl:value-of select="$banner"/>: <xsl:value-of select="inv:ID"/></h1>
          <ul>
            <xsl:for-each select="inv:Line">
              <li><xsl:value-of select="inv:Description"/> = <xsl:value-of select="inv:Amount"/> <xsl:value-of select="inv:Amount/@currencyID"/></li>
            </xsl:for-each>
          </ul>
          <p class="total"><xsl:value-of select="inv:PayableAmount"/></p>
        </body>
      </html>
    </xsl:template>
  </xsl:stylesheet>
XSL

xslt = Nokogiri::XSLT(xsl1)
params = Nokogiri::XSLT.quote_params(["banner", "EU e-Invoice"])
html_out = xslt.apply_to(doc, params)
raise "xslt html h1: #{html_out}" unless html_out.include?("EU e-Invoice: INV-2026-0001")
raise "xslt html line: #{html_out}" unless html_out.include?("Cloud Hosting") && html_out.include?("100.00") && html_out.include?("EUR")
raise "xslt html total" unless html_out.include?("125.00")

html_doc = xslt.transform(doc, params)
raise "transform returns HTML doc" unless html_doc.at_css("p.total").text == "125.00"

# 3. XSLT 2.0 Schematron-style business rule validation (sum(Line/Amount) == PayableAmount)
schematron_xsl = <<~XSL
  <?xml version="1.0" encoding="UTF-8"?>
  <xsl:stylesheet version="2.0"
      xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
      xmlns:xs="http://www.w3.org/2001/XMLSchema"
      xmlns:inv="urn:example:einvoice:1">
    <xsl:output method="xml" indent="yes"/>
    <xsl:template match="/inv:Invoice">
      <report>
        <xsl:variable name="sumLines" select="sum(for $a in inv:Line/inv:Amount return xs:decimal($a))"/>
        <xsl:variable name="payable" select="xs:decimal(inv:PayableAmount)"/>
        <xsl:if test="$sumLines != $payable">
          <failed-assert id="BR-CO-15">Sum of lines (<xsl:value-of select="$sumLines"/>) != PayableAmount (<xsl:value-of select="$payable"/>)</failed-assert>
        </xsl:if>
      </report>
    </xsl:template>
  </xsl:stylesheet>
XSL

sch = Nokogiri::XSLT(schematron_xsl)
ok_report = sch.transform(doc)
raise "schematron ok failed: #{ok_report.to_xml}" unless ok_report.xpath("//failed-assert").empty?

bad_math_doc = Nokogiri::XML(valid_xml.sub("125.00", "999.00"))
fail_report = sch.transform(bad_math_doc)
failed = fail_report.at_xpath("//failed-assert")
raise "expected BR-CO-15 failure" unless failed && failed["id"] == "BR-CO-15" && failed.text.include?("125")

# 4. XSD 1.1 assertion validation (xs:assert)
xsd11 = <<~XSD
  <?xml version="1.0" encoding="UTF-8"?>
  <xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
    <xs:element name="Order">
      <xs:complexType>
        <xs:sequence>
          <xs:element name="Item" type="xs:decimal" maxOccurs="unbounded"/>
          <xs:element name="Total" type="xs:decimal"/>
        </xs:sequence>
        <xs:assert test="Total = sum(Item)"/>
      </xs:complexType>
    </xs:element>
  </xs:schema>
XSD
s11 = Nokogiri::XML::Schema(xsd11)
raise "xsd11 valid" unless s11.valid?(Nokogiri::XML("<Order><Item>10</Item><Item>15</Item><Total>25</Total></Order>"))
raise "xsd11 invalid" if s11.valid?(Nokogiri::XML("<Order><Item>10</Item><Item>15</Item><Total>99</Total></Order>"))
`)
	if err != nil {
		t.Fatalf("e-invoice XSD/XSLT test failed: %v", err)
	}
}

func TestEInvoiceEmbeddedXMLDSigXAdESWithPKCS12(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "base64"
require "openssl"
require "nokogiri"

# 1. Create a tax-authority-style PKCS#12 (.p12) bundle with RSA-2048 key + X.509 cert
key = OpenSSL::PKey::RSA.new(2048)
cert = OpenSSL::X509::Certificate.new
cert.version = 2
cert.serial = 123456
cert.subject = OpenSSL::X509::Name.parse("/CN=Acme d.o.o./O=Acme/C=HR")
cert.issuer = cert.subject
cert.public_key = key.public_key
cert.not_before = Time.now - 60
cert.not_after = Time.now + 86400
cert.sign(key, OpenSSL::Digest::SHA256.new)

p12_der = OpenSSL::PKCS12.create("fiscal-pass", "Invoice Signer", key, cert).to_der

# Reload from .p12 bytes just like a real e-invoicing service
p12 = OpenSSL::PKCS12.new(p12_der, "fiscal-pass")
sign_key  = p12.key
sign_cert = p12.certificate
raise "p12 cert subject" unless sign_cert.subject.to_s.include?("CN=Acme d.o.o.")
raise "p12 cert serial" unless sign_cert.serial == 123456

# 2. Build an e-Invoice XML document
doc = Nokogiri::XML(<<~XML)
  <?xml version="1.0" encoding="UTF-8"?>
  <Invoice xmlns="urn:example:einvoice:1" Id="inv-1">
    <ID>INV-2026-0042</ID>
    <PayableAmount currencyID="EUR">250.00</PayableAmount>
  </Invoice>
XML

# 3. Compute Enveloped Signature Digest (Exclusive C14N of invoice without ds:Signature)
c14n_mode = Nokogiri::XML::XML_C14N_EXCLUSIVE_1_0
doc_digest = Base64.strict_encode64(OpenSSL::Digest::SHA256.digest(doc.root.canonicalize(c14n_mode)))

# Build XAdES-BES SignedProperties node & compute its digest
cert_digest = Base64.strict_encode64(OpenSSL::Digest::SHA256.digest(sign_cert.to_der))
cert_b64    = Base64.strict_encode64(sign_cert.to_der)

signed_props_xml = <<~XML
  <xades:SignedProperties xmlns:xades="http://uri.etsi.org/01903/v1.3.2#" xmlns:ds="http://www.w3.org/2000/09/xmldsig#" Id="xades-signed-props">
    <xades:SignedSignatureProperties>
      <xades:SigningTime>2026-11-23T10:00:00Z</xades:SigningTime>
      <xades:SigningCertificate>
        <xades:Cert>
          <xades:CertDigest>
            <ds:DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/>
            <ds:DigestValue>#{cert_digest}</ds:DigestValue>
          </xades:CertDigest>
          <xades:IssuerSerial>
            <ds:X509IssuerName>#{sign_cert.issuer.to_s}</ds:X509IssuerName>
            <ds:X509SerialNumber>#{sign_cert.serial}</ds:X509SerialNumber>
          </xades:IssuerSerial>
        </xades:Cert>
      </xades:SigningCertificate>
    </xades:SignedSignatureProperties>
  </xades:SignedProperties>
XML
sp_doc = Nokogiri::XML(signed_props_xml)
sp_digest = Base64.strict_encode64(OpenSSL::Digest::SHA256.digest(sp_doc.root.canonicalize(c14n_mode)))

# Build ds:SignedInfo & sign its Exclusive C14N with RSA-SHA256
signed_info_xml = <<~XML
  <ds:SignedInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
    <ds:CanonicalizationMethod Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/>
    <ds:SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"/>
    <ds:Reference URI="">
      <ds:Transforms>
        <ds:Transform Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"/>
        <ds:Transform Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/>
      </ds:Transforms>
      <ds:DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/>
      <ds:DigestValue>#{doc_digest}</ds:DigestValue>
    </ds:Reference>
    <ds:Reference Type="http://uri.etsi.org/01903#SignedProperties" URI="#xades-signed-props">
      <ds:Transforms>
        <ds:Transform Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/>
      </ds:Transforms>
      <ds:DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/>
      <ds:DigestValue>#{sp_digest}</ds:DigestValue>
    </ds:Reference>
  </ds:SignedInfo>
XML
si_doc = Nokogiri::XML(signed_info_xml)
sig_val = Base64.strict_encode64(sign_key.sign("SHA256", si_doc.root.canonicalize(c14n_mode)))

# Embed complete ds:Signature (with XAdES QualifyingProperties) inside the Invoice
sig_frag = Nokogiri::XML(<<~XML).root
  <ds:Signature xmlns:ds="http://www.w3.org/2000/09/xmldsig#" Id="sig-1">
    #{si_doc.root.to_xml}
    <ds:SignatureValue>#{sig_val}</ds:SignatureValue>
    <ds:KeyInfo>
      <ds:X509Data>
        <ds:X509Certificate>#{cert_b64}</ds:X509Certificate>
      </ds:X509Data>
    </ds:KeyInfo>
    <ds:Object>
      <xades:QualifyingProperties xmlns:xades="http://uri.etsi.org/01903/v1.3.2#" Target="#sig-1">
        #{sp_doc.root.to_xml}
      </xades:QualifyingProperties>
    </ds:Object>
  </ds:Signature>
XML
doc.root.add_child(sig_frag)
signed_xml = doc.to_xml

# 4. Verify the signed e-Invoice from serialized XML (receiver / tax authority side)
verify_invoice = lambda do |xml_text|
  d = Nokogiri::XML(xml_text)
  ns = { "ds" => "http://www.w3.org/2000/09/xmldsig#", "xades" => "http://uri.etsi.org/01903/v1.3.2#" }
  sig_node = d.at_xpath("//ds:Signature", ns)
  si_node  = sig_node.at_xpath("./ds:SignedInfo", ns)
  sv_b64   = sig_node.at_xpath("./ds:SignatureValue", ns).text.strip
  cert_der = Base64.decode64(sig_node.at_xpath(".//ds:X509Certificate", ns).text)
  x509     = OpenSSL::X509::Certificate.new(cert_der)

  # Verify SignedInfo RSA-SHA256 signature
  return false unless x509.public_key.verify("SHA256", Base64.decode64(sv_b64), si_node.canonicalize(c14n_mode))

  # Verify SignedProperties digest
  sp_ref_digest = si_node.at_xpath("./ds:Reference[@URI='#xades-signed-props']/ds:DigestValue", ns).text.strip
  sp_node = sig_node.at_xpath(".//xades:SignedProperties[@Id='xades-signed-props']", ns)
  calc_sp = Base64.strict_encode64(OpenSSL::Digest::SHA256.digest(sp_node.canonicalize(c14n_mode)))
  return false unless calc_sp == sp_ref_digest

  # Verify Enveloped Invoice digest (detach ds:Signature copy)
  doc_ref_digest = si_node.at_xpath("./ds:Reference[@URI='']/ds:DigestValue", ns).text.strip
  d_copy = d.dup
  d_copy.at_xpath("//ds:Signature", ns).unlink
  calc_doc = Base64.strict_encode64(OpenSSL::Digest::SHA256.digest(d_copy.root.canonicalize(c14n_mode)))
  calc_doc == doc_ref_digest
end

raise "signed invoice verification failed" unless verify_invoice.call(signed_xml)
raise "tampered invoice MUST fail verification" if verify_invoice.call(signed_xml.sub("250.00", "999.00"))
`)
	if err != nil {
		t.Fatalf("e-invoice XML-DSig/XAdES + PKCS12 test failed: %v", err)
	}
}
