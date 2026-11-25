package nokogiri

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "nokogiri", Version: "1.19.7", Feature: "nokogiri",
		Summary: "ORuby pure-Go Nokogiri (XML/HTML5/XPath/CSS/XSD/XSLT/RelaxNG)",
		Files:   []string{"lib/nokogiri.rb"},
	})
}
