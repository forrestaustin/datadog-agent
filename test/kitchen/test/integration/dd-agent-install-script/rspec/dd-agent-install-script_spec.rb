require 'spec_helper'

describe 'dd-agent-installation-script' do
  include_examples 'Agent'

  context 'when testing the install infos' do
    let(:install_info_path) do
      if os == :windows
        conf_path = "#{ENV['ProgramData']}\\Datadog\\install_info"
      else
        conf_path = '/etc/datadog-agent/install_info'
      end
    end

    let(:install_info) do
      YAML.load_file(install_info_path)
    end

    it 'adds an install_info' do
      expect(install_info['install_method']).to match(
        'name' => 'install_script',
        'tool' => 'install_script',
        'version' => /^\d+\.\d+\.\d+$/
      )
    end
  end
end
